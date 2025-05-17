package controllers

import (
	"context"
	"echo-mongo-api/configs"
	"echo-mongo-api/models"
	"echo-mongo-api/responses"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var memeCollection *mongo.Collection = configs.GetCollection(configs.DB, "memes")

func AddMeme(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var meme models.Meme

	// Validate request body
	if err := c.Bind(&meme); err != nil {
		return c.JSON(http.StatusBadRequest, responses.MemeResponse{
			Status:  http.StatusBadRequest,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	// Validate required fields
	if validationErr := validate.Struct(&meme); validationErr != nil {
		return c.JSON(http.StatusBadRequest, responses.MemeResponse{
			Status:  http.StatusBadRequest,
			Message: "error",
			Data:    &echo.Map{"data": validationErr.Error()},
		})
	}

	// Set timestamps if not provided
	if meme.CreatedTs == "" {
		meme.CreatedTs = time.Now().Format(time.RFC3339)
	}
	meme.UpdatedTs = time.Now().Format(time.RFC3339)

	// Insert meme
	result, err := memeCollection.InsertOne(ctx, meme)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, responses.MemeResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	return c.JSON(http.StatusCreated, responses.MemeResponse{
		Status:  http.StatusCreated,
		Message: "success",
		Data:    &echo.Map{"data": result.InsertedID},
	})
}

func GetMemesByBid(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	bid := c.Param("bid")

	// Build filter
	filter := bson.M{"_bid": bid}

	// Find memes
	cursor, err := memeCollection.Find(ctx, filter)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, responses.MemeResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}
	defer cursor.Close(ctx)

	// Decode results
	var memes []models.Meme
	if err = cursor.All(ctx, &memes); err != nil {
		return c.JSON(http.StatusInternalServerError, responses.MemeResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	return c.JSON(http.StatusOK, responses.MemeResponse{
		Status:  http.StatusOK,
		Message: "success",
		Data:    &echo.Map{"data": memes},
	})
}

func GetAllMemes(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Find all memes with empty filter
	cursor, err := memeCollection.Find(ctx, bson.M{})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, responses.MemeResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}
	defer cursor.Close(ctx)

	// Decode results
	var memes []models.Meme
	if err = cursor.All(ctx, &memes); err != nil {
		return c.JSON(http.StatusInternalServerError, responses.MemeResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	return c.JSON(http.StatusOK, responses.MemeResponse{
		Status:  http.StatusOK,
		Message: "success",
		Data:    &echo.Map{"data": memes},
	})
}
