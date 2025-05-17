// services/worker_service.go
package services

import (
	"context"
	"echo-mongo-api/configs"
	"echo-mongo-api/models"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type WorkerService struct {
	taskService   *TaskService
	helperService *HelperService
	redisClient   *redis.Client
	maxRetries    int
	retryDelay    time.Duration
	logger        *slog.Logger
	debug         bool
}

func NewWorkerService(
	taskService *TaskService,
	helperService *HelperService,
	redisClient *redis.Client,
) *WorkerService {
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	)

	return &WorkerService{
		taskService:   taskService,
		helperService: helperService,
		redisClient:   redisClient,
		maxRetries:    3,               // Maximum number of retries
		retryDelay:    5 * time.Second, // Initial delay between retries
		logger:        logger,
		debug:         true,
	}
}

func (ws *WorkerService) StartWorker(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				ws.processTask(ctx)
				time.Sleep(1 * time.Second)
			}
		}
	}()
}

func (ws *WorkerService) processTask(ctx context.Context) {
	// Get task from Redis queue
	result, err := ws.redisClient.BRPop(ctx, 30*time.Second, "task_queue").Result()
	if err != nil {
		if err != redis.Nil {
			ws.logger.Error("Failed to get task from queue", "error", err)
		}
		return
	}

	if len(result) < 2 {
		ws.logger.Error("Invalid task data from queue")
		return
	}

	taskData := result[1]
	var task models.Task
	err = json.Unmarshal([]byte(taskData), &task)
	if err != nil {
		ws.logger.Error("Failed to unmarshal task", "error", err)
		return
	}

	// Update task status to processing
	err = ws.taskService.UpdateTaskStatus(ctx, task.ID, models.TaskStatusProcessing, nil, "")
	if err != nil {
		ws.logger.Error("Failed to update task status", "taskID", task.ID, "error", err)
		return
	}

	// Process task based on type
	switch task.Type {
	case "buy_tokens":
		ws.processBuyTokensTask(ctx, task)
	default:
		ws.logger.Error("Unknown task type", "type", task.Type)
		err = ws.taskService.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed, nil, "unknown task type")
		if err != nil {
			ws.logger.Error("Failed to update task status", "taskID", task.ID, "error", err)
		}
	}
}

func (ws *WorkerService) processBuyTokensTask(ctx context.Context, task models.Task) {
	var payload models.BuyTokensTaskPayload
	err := json.Unmarshal([]byte(task.Payload.(string)), &payload)
	if err != nil {
		ws.logger.Error("Failed to unmarshal buy tokens payload", "error", err)
		_ = ws.taskService.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed, nil, "invalid payload")
		return
	}

	// Process token transfers and DB updates here
	// This is where you would move the logic from the original BuyTokens endpoint

	// Example processing:
	relayer, err := solana.PrivateKeyFromBase58(configs.EnvRelayerPrivateKey())
	if err != nil {
		ws.logger.Error("Failed to load relayer private key", "error", err)
		_ = ws.taskService.UpdateTaskStatus(ctx, task.ID, models.TaskStatusFailed, nil, "failed to load relayer key")
		return
	}

	// Process each transaction with retry logic
	for _, txID := range payload.TransactionIDs {
		retryCount := 0
		var lastError error

		for retryCount < ws.maxRetries {
			err := ws.processTransactionWithRetry(ctx, txID, relayer.PublicKey(), payload.BundleID, payload.UserPublicKey)
			if err == nil {
				break // Success, move to next transaction
			}

			lastError = err
			retryCount++
			ws.logger.Warn("Transaction processing failed, retrying",
				"txID", txID,
				"retryCount", retryCount,
				"error", err)

			// Exponential backoff
			time.Sleep(ws.retryDelay * time.Duration(retryCount))
		}

		if retryCount >= ws.maxRetries {
			ws.logger.Error("Max retries exceeded for transaction",
				"txID", txID,
				"error", lastError)
			// Continue with next transaction even if this one fails
		}
	}

	// Update task status based on overall success
	err = ws.taskService.UpdateTaskStatus(ctx, task.ID, models.TaskStatusCompleted, "processing completed", "")
	if err != nil {
		ws.logger.Error("Failed to update task status", "taskID", task.ID, "error", err)
	}
}

func (ws *WorkerService) processTransactionWithRetry(
	ctx context.Context,
	txID string,
	relayerPubkey solana.PublicKey,
	bundleID int,
	userWallet string,
) error {
	// Get transaction details
	txSig := solana.MustSignatureFromBase58(txID)
	txDetails, err := ws.helperService.rpcClient.GetTransaction(ctx, txSig, nil)
	if err != nil {
		return fmt.Errorf("failed to get transaction details: %w", err)
	}

	// Process balance changes with additional error handling
	err = ws.processBalanceChanges(ctx, txDetails, relayerPubkey, bundleID, userWallet)
	if err != nil {
		return fmt.Errorf("failed to process balance changes: %w", err)
	}

	return nil
}

// Enhanced processBalanceChanges with transaction support
func (ws *WorkerService) processBalanceChanges(
	ctx context.Context,
	txDetails *rpc.GetTransactionResult,
	relayerPubkey solana.PublicKey,
	bundleID int,
	userWallet string,
) error {
	session, err := ws.taskService.mongoClient.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start MongoDB session: %w", err)
	}
	defer session.EndSession(ctx)

	// Use transaction for atomic updates
	err = mongo.WithSession(ctx, session, func(sessionContext mongo.SessionContext) error {
		if err := session.StartTransaction(); err != nil {
			return fmt.Errorf("failed to start transaction: %w", err)
		}

		// Get bundle details
		var bundle models.Bundle
		bundleCollection := ws.taskService.mongoClient.Database("db-wolfindex").Collection("bundles")
		err := bundleCollection.FindOne(sessionContext, bson.M{"_bid": bundleID}).Decode(&bundle)
		if err != nil {
			session.AbortTransaction(sessionContext)
			return fmt.Errorf("failed to find bundle: %w", err)
		}

		// Get or create user deposit
		userDepositCollection := ws.taskService.mongoClient.Database("db-wolfindex").Collection("user_deposits")
		var existingDeposit models.UserDeposit
		err = userDepositCollection.FindOne(sessionContext, bson.M{"wallet": userWallet}).Decode(&existingDeposit)
		if err != nil && err != mongo.ErrNoDocuments {
			session.AbortTransaction(sessionContext)
			return fmt.Errorf("failed to query user deposit: %w", err)
		}

		// Process the transaction and update balances
		// ... (your existing balance change processing logic) ...

		if err := session.CommitTransaction(sessionContext); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}

	return nil
}
