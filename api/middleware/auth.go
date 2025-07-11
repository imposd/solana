package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"nova/api/types"
)

func AuthMiddleware(mongoClient *mongo.Client) fiber.Handler {
	return func(c *fiber.Ctx) error {
		apiKey := c.Get("X-API-Key")

		if apiKey == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(types.ErrorResponse{
				Success: false,
				Message: "API key is required",
			})
		}

		collection := mongoClient.Database("nova").Collection("api_keys")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

		defer cancel()

		var keyDoc types.APIKey
		err := collection.FindOne(ctx, bson.M{"key": apiKey, "active": true}).Decode(&keyDoc)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				return c.Status(fiber.StatusUnauthorized).JSON(types.ErrorResponse{
					Success: false,
					Message: "Invalid API key",
				})
			}
			return c.Status(fiber.StatusInternalServerError).JSON(types.ErrorResponse{
				Success: false,
				Message: "Database error",
			})
		}

		c.Locals("api_key", keyDoc.Key)

		return c.Next()
	}
}
