package config

import (
	"fmt"
	"log"
	"nova/api/types"
	"os"
	"time"
)

func Load() *types.Config {
	heliusAPIKey := getEnv("HELIUS_API_KEY", "")
	if heliusAPIKey == "" {
		log.Fatal("HELIUS_API_KEY environment variable is required")
	}
	
	solanaRPCURL := getEnv("SOLANA_RPC_URL", fmt.Sprintf("https://pomaded-lithotomies-xfbhnqagbt-dedicated.helius-rpc.com/?api-key=%s", heliusAPIKey))

	return &types.Config{
		Port:         getEnv("PORT", "3000"),
		MongoURI:     getEnv("MONGO_URI", "mongodb://localhost:27017"),
		RedisURI:     getEnv("REDIS_URI", "localhost:6379"),
		SolaanRPCURL: solanaRPCURL,
		CacheTTL:     10 * time.Second,
		RateLimit:    10,
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
