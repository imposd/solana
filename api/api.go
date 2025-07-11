package api

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"

	"nova/api/config"
	"nova/api/database"
	"nova/api/routes"
	"nova/api/services"
)

func Main() {
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error loading .env file, falling back to environment variables")
	}

	cfg := config.Load()
	db := database.New(cfg)

	solanaService := services.NewSolanaService(cfg.SolaanRPCURL, db.Redis)
	routes.InitSolanaService(solanaService)

	app := fiber.New(fiber.Config{
		JSONEncoder: json.Marshal,
		JSONDecoder: json.Unmarshal,
	})

	app.Use(recover.New())
	app.Use(logger.New())

	routes.InitRoutes(app, db)

	port := os.Getenv("API_PORT")

	if port == "" {
		port = "8080"
	}

	fmt.Println("API is up and running on port", port)
	log.Fatal(app.Listen("0.0.0.0:" + port))
}
