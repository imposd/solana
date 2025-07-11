package middleware

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"nova/api/types"
)

func AuthMiddleware(db *types.Database) fiber.Handler {
	collection := db.MongoDB.Database("nova").Collection("api_keys")

	return func(c *fiber.Ctx) error {
		apiKey := c.Get("X-API-Key")

		if apiKey == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(types.ErrorResponse{
				Success: false,
				Message: "API key is required",
			})
		}

		cacheKey := "api_key:" + apiKey
		ctx := context.Background()

		redisCtx, redisCancel := context.WithTimeout(ctx, 100*time.Millisecond)
		cached, err := db.Redis.Get(redisCtx, cacheKey).Result()

		redisCancel()

		if err == nil {
			if cached == "valid" {
				c.Locals("api_key", apiKey)
				return c.Next()
			}

			if cached == "invalid" {
				return c.Status(fiber.StatusUnauthorized).JSON(types.ErrorResponse{
					Success: false,
					Message: "Invalid API key",
				})
			}
		}

		mongoCtx, mongoCancel := context.WithTimeout(ctx, 1*time.Second)
		defer mongoCancel()

		var keyDoc types.APIKey
		err = collection.FindOne(mongoCtx, bson.M{"key": apiKey, "active": true}).Decode(&keyDoc)

		if err != nil {
			if err == mongo.ErrNoDocuments {
				go func() {
					bgCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
					defer cancel()
					db.Redis.Set(bgCtx, cacheKey, "invalid", 5*time.Minute)
				}()
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

		go func() {
			bgCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			db.Redis.Set(bgCtx, cacheKey, "valid", 15*time.Minute)
		}()

		c.Locals("api_key", keyDoc.Key)
		return c.Next()
	}
}
