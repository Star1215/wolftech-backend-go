package routes

import (
	"echo-mongo-api/controllers"

	"github.com/labstack/echo/v4"
)

func BundleRoute(g *echo.Group) {
	g.POST("/addBundle", controllers.AddBundle)
	g.GET("/bundles", controllers.GetAllBundles)
}
