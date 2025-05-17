# This is the backend project
# Envirionment
go mod init echo-mongo-api
go get -u github.com/labstack/echo/v4 go.mongodb.org/mongo-driver/mongo github.com/joho/godotenv github.com/go-playground/validator/v10
go install github.com/air-verse/air@latest

go mod tidy