package routes

import (
	"github.com/gofiber/fiber/v2"

	"nova/api/middleware"
	"nova/api/types"
)

func InitRoutes(app *fiber.App, db *types.Database) {
	api := app.Group("/api")

	api.Use(middleware.RateLimitMiddleware())
	api.Use(middleware.AuthMiddleware(db))

	api.Post("/get-balance", GetBalance)
}
