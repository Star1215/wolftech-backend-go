package responses

import "github.com/labstack/echo/v4"

type TokenResponse struct {
	Status  int       `json:"status"`
	Message string    `json:"message"`
	Data    *echo.Map `json:"data"`
}
