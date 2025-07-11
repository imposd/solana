package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"nova/api/types"
)

func main() {
	err := godotenv.Load(".env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("MONGO_URI")))

	if err != nil {
		log.Fatal("Error connecting to MongoDB:", err)
	}

	defer client.Disconnect(ctx)

	apiKey := generateAPIKey()

	collection := client.Database("nova").Collection("api_keys")

	keyDoc := types.APIKey{
		Key:       apiKey,
		Active:    true,
		CreatedAt: time.Now(),
	}

	_, err = collection.InsertOne(ctx, keyDoc)

	if err != nil {
		log.Fatal("Error inserting API key:", err)
	}

	fmt.Printf("API Key created: %s\n", apiKey)
}

func generateAPIKey() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
