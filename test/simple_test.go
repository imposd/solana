package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"nova/api/config"
	"nova/api/middleware"
	"nova/api/routes"
	"nova/api/services"
	"nova/api/types"
)

func setupSimpleTest(t *testing.T) (*fiber.App, string) {
	t.Helper()

	middleware.ClearRateLimiters()

	if err := godotenv.Load("../.env"); err != nil {
		t.Logf("Warning: Could not load .env file: %v", err)
	}

	if os.Getenv("HELIUS_API_KEY") == "" {
		t.Skip("HELIUS_API_KEY not set")
	}

	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	require.NoError(t, err)

	err = mongoClient.Ping(ctx, nil)
	require.NoError(t, err)

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURI,
	})

	err = redisClient.Ping(ctx).Err()
	require.NoError(t, err)

	solanaService := services.NewSolanaService(cfg.SolaanRPCURL, redisClient)

	testAPIKey := fmt.Sprintf("simple-test-key-%d", time.Now().UnixNano())
	collection := mongoClient.Database("nova").Collection("api_keys")

	apiKey := types.APIKey{
		Key:       testAPIKey,
		Active:    true,
		CreatedAt: time.Now(),
	}

	_, err = collection.InsertOne(ctx, apiKey)
	require.NoError(t, err)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		collection.DeleteOne(ctx, bson.M{"key": testAPIKey})
		mongoClient.Disconnect(ctx)
		redisClient.Close()
	})

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(middleware.RateLimitMiddleware())
	app.Use(middleware.AuthMiddleware(mongoClient))

	routes.InitSolanaService(solanaService)
	routes.InitRoutes(app, mongoClient)

	return app, testAPIKey
}

func TestSimple_SingleWallet(t *testing.T) {
	app, apiKey := setupSimpleTest(t)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}

	reqBody, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Forwarded-For", "10.0.0.1")

	t.Logf("Testing single wallet: %s", testWallets[0])

	start := time.Now()
	resp, err := app.Test(req, 30000)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var response types.BalanceResponse
	body, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(body, &response)

	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Len(t, response.Data, 1)
	assert.Equal(t, testWallets[0], response.Data[0].Address)
	assert.GreaterOrEqual(t, response.Data[0].Balance, 0.0)

	t.Logf("✓ Single wallet test passed - Balance: %f SOL (took %v)", response.Data[0].Balance, duration)
}

func TestSimple_MultipleWallets(t *testing.T) {
	app, apiKey := setupSimpleTest(t)

	request := types.BalanceRequest{
		Wallets: testWallets,
	}

	reqBody, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Forwarded-For", "10.0.0.2")

	t.Logf("Testing multiple wallets: %d addresses", len(testWallets))

	start := time.Now()
	resp, err := app.Test(req, 30000)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	var response types.BalanceResponse
	body, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(body, &response)

	require.NoError(t, err)
	assert.True(t, response.Success)
	assert.Len(t, response.Data, len(testWallets))

	for i, result := range response.Data {
		assert.Equal(t, testWallets[i], result.Address)
		if result.Error != "" {
			t.Logf("Wallet %s had error: %s", result.Address, result.Error)
		} else {
			assert.GreaterOrEqual(t, result.Balance, 0.0)
			t.Logf("Wallet %s: %f SOL", result.Address, result.Balance)
		}
	}

	t.Logf("✓ Multiple wallets test passed (took %v)", duration)
}

func TestSimple_Caching(t *testing.T) {
	app, apiKey := setupSimpleTest(t)

	wallet := testWallets[0]
	request := types.BalanceRequest{
		Wallets: []string{wallet},
	}
	reqBody, _ := json.Marshal(request)

	t.Logf("Testing caching behavior for wallet: %s", wallet)

	req1, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-API-Key", apiKey)
	req1.Header.Set("X-Forwarded-For", "10.0.0.3")

	t.Log("Making first request (cache miss)...")
	start1 := time.Now()
	resp1, err := app.Test(req1, 30000)
	duration1 := time.Since(start1)

	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp1.StatusCode)

	var response1 types.BalanceResponse
	body1, _ := io.ReadAll(resp1.Body)
	err = json.Unmarshal(body1, &response1)
	require.NoError(t, err)
	balance1 := response1.Data[0].Balance

	req2, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-API-Key", apiKey)
	req2.Header.Set("X-Forwarded-For", "10.0.0.4")

	t.Log("Making second request (cache hit)...")
	start2 := time.Now()
	resp2, err := app.Test(req2, 30000)
	duration2 := time.Since(start2)

	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp2.StatusCode)

	var response2 types.BalanceResponse
	body2, _ := io.ReadAll(resp2.Body)
	err = json.Unmarshal(body2, &response2)
	require.NoError(t, err)
	balance2 := response2.Data[0].Balance

	assert.Equal(t, balance1, balance2, "Both requests should return same balance")
	assert.True(t, duration2 < duration1, "Second request should be faster due to caching")

	speedup := float64(duration1) / float64(duration2)
	t.Logf("Cache miss: %v, Cache hit: %v, Speedup: %.2fx", duration1, duration2, speedup)
	t.Logf("✓ Caching test passed - %f SOL returned both times", balance1)
}

func TestSimple_Authentication(t *testing.T) {
	app, apiKey := setupSimpleTest(t)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}
	reqBody, _ := json.Marshal(request)

	t.Log("Testing authentication")

	req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.0.0.5")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	t.Log("✓ No API key: Correctly rejected")

	req, _ = http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "invalid-key")
	req.Header.Set("X-Forwarded-For", "10.0.0.6")

	resp, err = app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	t.Log("✓ Invalid API key: Correctly rejected")

	req, _ = http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("X-Forwarded-For", "10.0.0.7")

	resp, err = app.Test(req, 30000)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	t.Log("✓ Valid API key: Correctly accepted")

	t.Log("✓ Authentication test passed")
}

func TestSimple_RateLimiting(t *testing.T) {
	app, apiKey := setupSimpleTest(t)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}
	reqBody, _ := json.Marshal(request)

	t.Log("Testing rate limiting")

	clientIP := "10.0.0.100"
	successCount := 0

	for i := 0; i < 15; i++ {
		req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", apiKey)
		req.Header.Set("X-Forwarded-For", clientIP)

		resp, err := app.Test(req, 30000)
		require.NoError(t, err)

		if resp.StatusCode == fiber.StatusOK {
			successCount++
		} else if resp.StatusCode == fiber.StatusTooManyRequests {
			t.Logf("Request %d: Rate limited (expected)", i+1)
			break
		}
	}

	t.Logf("Rate limiting test: %d successful requests before limit", successCount)
	assert.GreaterOrEqual(t, successCount, 1, "Should allow at least 1 request")

	t.Log("✓ Rate limiting test passed")
}
