package main

import (
	"context"
	"echo-mongo-api/configs"
	"echo-mongo-api/routes"
	"echo-mongo-api/services"
	"log"
	"os/signal"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/ws"
	"github.com/go-redis/redis/v8"
	"github.com/ilkamo/jupiter-go/jupiter"
	jitorpc "github.com/jito-labs/jito-go-rpc"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {

	// Create context that listens for interrupt signals
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	e := echo.New()

	// Configure CORS middleware
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"http://localhost:3000" /*"https://wolftech-backend.onrender.com/api"*/},
		AllowCredentials: true,
		MaxAge:           86400, // 24 hours
	}))
	//run database
	configs.ConnectDB()

	// Initialize Redis with retry logic
	var redisClient *redis.Client
	maxRedisRetries := 5
	for i := 0; i < maxRedisRetries; i++ {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     configs.EnvRedisAddr(), // e.g., "localhost:6379"
			Password: configs.EnvRedisPassword(),
			DB:       0,
		})

		_, err := redisClient.Ping(ctx).Result()
		if err == nil {
			break
		}

		if i == maxRedisRetries-1 {
			log.Fatalf("Failed to connect to Redis after %d attempts: %v", maxRedisRetries, err)
		}

		log.Printf("Redis connection failed (attempt %d/%d), retrying...", i+1, maxRedisRetries)
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	// Initialize Solana clients with retry logic
	var rpcClient *rpc.Client
	var wsClient *ws.Client
	var err error

	maxSolanaRetries := 3
	for i := 0; i < maxSolanaRetries; i++ {
		rpcClient = rpc.New(configs.EnvSolanaRPC())
		wsClient, err = ws.Connect(ctx, rpc.MainNetBeta_WS)
		if err == nil {
			break
		}

		if i == maxSolanaRetries-1 {
			log.Fatalf("Failed to connect to Solana after %d attempts: %v", maxSolanaRetries, err)
		}

		log.Printf("Solana connection failed (attempt %d/%d), retrying...", i+1, maxSolanaRetries)
		time.Sleep(time.Duration(i+1) * time.Second)
	}

	// Initialize Jupiter client
	jupClient, err := jupiter.NewClientWithResponses(jupiter.DefaultAPIURL)
	if err != nil {
		panic(err)
	}

	// Initialize Jito client
	jitoClient := jitorpc.NewJitoJsonRpcClient(configs.EnvJitoAPI(), "")

	// Initialize services
	jupiterService := services.NewJupiterService(jupClient)
	helperService := services.NewHelperService(rpcClient, wsClient)
	jitoService := services.NewJitoService(jitoClient)
	taskService := services.NewTaskService(redisClient, configs.DB)
	workerService := services.NewWorkerService(taskService, helperService, redisClient)

	// Recover stale tasks on startup
	if err := taskService.RecoverStaleTasks(ctx); err != nil {
		log.Printf("Warning: failed to recover stale tasks: %v", err)
	}

	// Start background workers
	workerService.StartWorker(ctx)

	//routes
	api := e.Group("/api") // Create API route group
	routes.UserRoute(api)
	routes.BundleRoute(api)
	routes.MemeRoute(api)
	routes.TokenRoute(
		api,
		jupiterService,
		helperService,
		jitoService,
		taskService,
		rpcClient,
		wsClient,
		redisClient,
	)

	// Start server in a goroutine
	go func() {
		port := configs.EnvPORT()
		log.Printf("Server starting at :%s", port)
		if err := e.Start(":" + port); err != nil {
			log.Printf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-ctx.Done()

	// Graceful shutdown
	log.Println("Shutting down server...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
	}

	log.Println("Server stopped")
}
