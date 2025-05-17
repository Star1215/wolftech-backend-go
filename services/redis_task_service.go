// services/task_service.go
package services

import (
	"context"
	"echo-mongo-api/models"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type TaskService struct {
	redisClient *redis.Client
	mongoClient *mongo.Client
	logger      *slog.Logger
	debug       bool
}

func NewTaskService(redisClient *redis.Client, mongoClient *mongo.Client) *TaskService {
	logger := slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		}),
	)

	return &TaskService{
		redisClient: redisClient,
		mongoClient: mongoClient,
		logger:      logger,
		debug:       true,
	}
}

func (ts *TaskService) CreateTask(ctx context.Context, taskType string, payload interface{}) (string, error) {
	taskID := generateTaskID()
	task := models.Task{
		ID:        taskID,
		Type:      taskType,
		Status:    models.TaskStatusPending,
		Payload:   payload,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Store in MongoDB
	collection := ts.mongoClient.Database("db-wolfindex").Collection("tasks")
	_, err := collection.InsertOne(ctx, task)
	if err != nil {
		return "", fmt.Errorf("failed to create task: %w", err)
	}

	// Add to Redis queue
	taskBytes, err := json.Marshal(task)
	if err != nil {
		return "", fmt.Errorf("failed to marshal task: %w", err)
	}

	err = ts.redisClient.LPush(ctx, "task_queue", taskBytes).Err()
	if err != nil {
		return "", fmt.Errorf("failed to add task to queue: %w", err)
	}

	return taskID, nil
}

func (ts *TaskService) GetTaskStatus(ctx context.Context, taskID string) (*models.Task, error) {
	collection := ts.mongoClient.Database("db-wolfindex").Collection("tasks")
	var task models.Task
	err := collection.FindOne(ctx, bson.M{"_id": taskID}).Decode(&task)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	return &task, nil
}

func (ts *TaskService) UpdateTaskStatus(ctx context.Context, taskID string, status models.TaskStatus, result interface{}, errMsg string) error {
	collection := ts.mongoClient.Database("db-wolfindex").Collection("tasks")
	update := bson.M{
		"$set": bson.M{
			"status":    status,
			"updatedAt": time.Now(),
			"result":    result,
			"error":     errMsg,
		},
	}

	if status == models.TaskStatusCompleted || status == models.TaskStatusFailed {
		now := time.Now()
		update["$set"].(bson.M)["completedAt"] = &now
	}

	_, err := collection.UpdateOne(ctx, bson.M{"_id": taskID}, update)
	if err != nil {
		return fmt.Errorf("failed to update task: %w", err)
	}
	return nil
}

func generateTaskID() string {
	return fmt.Sprintf("task_%d", time.Now().UnixNano())
}

// Recovery method to handle tasks that were interrupted
func (ts *TaskService) RecoverStaleTasks(ctx context.Context) error {
	collection := ts.mongoClient.Database("db-wolfindex").Collection("tasks")

	// Find all tasks that were processing but not completed
	filter := bson.M{
		"status": models.TaskStatusProcessing,
		"updatedAt": bson.M{
			"$lt": time.Now().Add(-15 * time.Minute), // Tasks stuck for more than 15 minutes
		},
	}

	update := bson.M{
		"$set": bson.M{
			"status":    models.TaskStatusFailed,
			"error":     "recovered from stale state",
			"updatedAt": time.Now(),
		},
	}

	// Update all stale tasks
	_, err := collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to recover stale tasks: %w", err)
	}

	// Get pending tasks and requeue them
	pendingFilter := bson.M{"status": models.TaskStatusPending}
	cursor, err := collection.Find(ctx, pendingFilter)
	if err != nil {
		return fmt.Errorf("failed to find pending tasks: %w", err)
	}
	defer cursor.Close(ctx)

	var tasks []models.Task
	if err := cursor.All(ctx, &tasks); err != nil {
		return fmt.Errorf("failed to decode pending tasks: %w", err)
	}

	for _, task := range tasks {
		if err := ts.requeueTask(ctx, task); err != nil {
			ts.logger.Error("Failed to requeue task", "taskID", task.ID, "error", err)
			continue
		}
	}

	return nil
}

func (ts *TaskService) requeueTask(ctx context.Context, task models.Task) error {
	taskBytes, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	err = ts.redisClient.LPush(ctx, "task_queue", taskBytes).Err()
	if err != nil {
		return fmt.Errorf("failed to add task to queue: %w", err)
	}

	ts.logger.Info("Requeued task", "taskID", task.ID)
	return nil
}
