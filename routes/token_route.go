package routes

import (
	"echo-mongo-api/controllers"
	"echo-mongo-api/services"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/gagliardetto/solana-go/rpc/ws"
	"github.com/go-redis/redis/v8"
	"github.com/labstack/echo/v4"
)

func TokenRoute(
	g *echo.Group,
	jupiterService *services.JupiterService,
	helperService *services.HelperService,
	jitoService *services.JitoService,
	taskService *services.TaskService,
	rpcClient *rpc.Client,
	wsClient *ws.Client,
	redisClient *redis.Client) {
	tokenController := controllers.NewTokenController(helperService, jupiterService, jitoService, taskService, rpcClient, wsClient, redisClient)

	g.POST("/buy-tokens", tokenController.BuyTokens)
	g.GET("/task-status/:taskId", tokenController.GetTaskStatus)
}
