package routes

import (
	"echo-mongo-api/controllers"

	"github.com/labstack/echo/v4"
)

func MemeRoute(g *echo.Group) {
	g.POST("/addMeme", controllers.AddMeme)
	g.GET("/getMemes/:bid", controllers.GetMemesByBid)
	g.GET("/memes", controllers.GetAllMemes)
}
