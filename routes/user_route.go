package routes

import (
	"echo-mongo-api/controllers"

	"github.com/labstack/echo/v4"
)

func UserRoute(g *echo.Group) {
	g.POST("/user", controllers.CreateUser)
	g.GET("/user/:userId", controllers.GetAUser)
	g.PUT("/user/:userId", controllers.EditAUser)
	g.DELETE("/user/:userId", controllers.DeleteAUser)
	g.GET("/users", controllers.GetAllUsers)
}
