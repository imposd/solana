package routes

import (
	"github.com/gofiber/fiber/v2"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"nova/api/middleware"
)

func InitRoutes(app *fiber.App, mongoClient *mongo.Client) {
	api := app.Group("/api")

	api.Use(middleware.RateLimitMiddleware())
	api.Use(middleware.AuthMiddleware(mongoClient))

	api.Post("/get-balance", GetBalance)
}
