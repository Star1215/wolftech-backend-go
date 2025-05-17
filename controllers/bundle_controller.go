package controllers

import (
	"context"
	"echo-mongo-api/configs"
	"echo-mongo-api/models"
	"echo-mongo-api/responses"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var bundleCollection *mongo.Collection = configs.GetCollection(configs.DB, "bundles")

func AddBundle(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var bundle models.Bundle
	log.Printf("AddBundle :")
	// Validate the request body
	if err := c.Bind(&bundle); err != nil {
		log.Fatal(err)
		return c.JSON(http.StatusBadRequest, responses.BundleResponse{
			Status:  http.StatusBadRequest,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	// Use validator library to validate required fields
	if validationErr := validate.Struct(&bundle); validationErr != nil {
		log.Fatal(validationErr)
		return c.JSON(http.StatusBadRequest, responses.BundleResponse{
			Status:  http.StatusBadRequest,
			Message: "error",
			Data:    &echo.Map{"data": validationErr.Error()},
		})
	}

	// Validate timestamp (must be a numeric string)
	if _, err := strconv.ParseInt(bundle.Timestamp, 10, 64); err != nil {
		log.Fatal(err)
		return c.JSON(http.StatusBadRequest, responses.BundleResponse{
			Status:  http.StatusBadRequest,
			Message: "Invalid timestamp format (must be Unix timestamp as string)",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	// If timestamp is empty, set current time
	if bundle.Timestamp == "" {
		bundle.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	}

	newBundle := models.Bundle{
		ID:              primitive.NewObjectID(),
		Bid:             bundle.Bid,
		BundleMeta:      bundle.BundleMeta,
		BundleAddress:   bundle.BundleAddress,
		BundleMemesInfo: bundle.BundleMemesInfo,
		Timestamp:       bundle.Timestamp,
	}

	// Insert the bundle
	result, err := bundleCollection.InsertOne(ctx, newBundle)
	if err != nil {
		log.Fatal(err)
		return c.JSON(http.StatusInternalServerError, responses.BundleResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}
	// Return the inserted ID
	return c.JSON(http.StatusCreated, responses.BundleResponse{
		Status:  http.StatusCreated,
		Message: "success",
		Data:    &echo.Map{"data": result.InsertedID},
	})
}

// Get all bundles
func GetAllBundles(c echo.Context) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var bundles []models.Bundle

	// Find all bundles
	cursor, err := bundleCollection.Find(ctx, bson.M{})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, responses.BundleResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}
	defer cursor.Close(ctx)

	// Iterate through the cursor and decode each bundle
	for cursor.Next(ctx) {
		var bundle models.Bundle
		if err := cursor.Decode(&bundle); err != nil {
			return c.JSON(http.StatusInternalServerError, responses.BundleResponse{
				Status:  http.StatusInternalServerError,
				Message: "error",
				Data:    &echo.Map{"data": err.Error()},
			})
		}
		bundles = append(bundles, bundle)
	}

	// Check for cursor errors
	if err := cursor.Err(); err != nil {
		return c.JSON(http.StatusInternalServerError, responses.BundleResponse{
			Status:  http.StatusInternalServerError,
			Message: "error",
			Data:    &echo.Map{"data": err.Error()},
		})
	}

	return c.JSON(http.StatusOK, responses.BundleResponse{
		Status:  http.StatusOK,
		Message: "success",
		Data:    &echo.Map{"data": bundles},
	})
}
