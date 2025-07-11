package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
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

func setupTest(t *testing.T) *TestSuite {
	t.Helper()

	middleware.ClearRateLimiters()

	if err := godotenv.Load("../.env"); err != nil {
		t.Logf("Warning: Could not load .env file: %v", err)
	}

	if os.Getenv("HELIUS_API_KEY") == "" {
		t.Skip("HELIUS_API_KEY not set - skipping tests")
	}

	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	require.NoError(t, err, "MongoDB connection should succeed")

	err = mongoClient.Ping(ctx, nil)
	require.NoError(t, err, "MongoDB ping should succeed")

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURI,
	})

	err = redisClient.Ping(ctx).Err()
	require.NoError(t, err, "Redis ping should succeed")

	solanaService := services.NewSolanaService(cfg.SolaanRPCURL, redisClient)

	testAPIKey := "test-api-key-123"
	collection := mongoClient.Database("nova").Collection("api_keys")

	collection.DeleteOne(ctx, bson.M{"key": testAPIKey})

	apiKey := types.APIKey{
		Key:       testAPIKey,
		Active:    true,
		CreatedAt: time.Now(),
	}

	_, err = collection.InsertOne(ctx, apiKey)
	require.NoError(t, err, "API key insertion should succeed")

	db := &types.Database{
		MongoDB: mongoClient,
		Redis:   redisClient,
	}

	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
	})

	app.Use(middleware.RateLimitMiddleware())
	app.Use(middleware.AuthMiddleware(db))

	routes.InitSolanaService(solanaService)
	routes.InitRoutes(app, db)

	return &TestSuite{
		app:           app,
		mongoClient:   mongoClient,
		redisClient:   redisClient,
		solanaService: solanaService,
		testAPIKey:    testAPIKey,
		cfg:           cfg,
	}
}

func (ts *TestSuite) cleanup(t *testing.T) {
	t.Helper()

	if ts.mongoClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		collection := ts.mongoClient.Database("nova").Collection("api_keys")
		collection.DeleteOne(ctx, bson.M{"key": ts.testAPIKey})
		ts.mongoClient.Disconnect(ctx)
	}

	if ts.redisClient != nil {
		ts.redisClient.Close()
	}
}

func TestAPI_SingleWallet(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}

	reqBody, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", ts.testAPIKey)

	t.Logf("Testing single wallet: %s", testWallets[0])

	start := time.Now()
	resp, err := ts.app.Test(req, 30000)
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

func TestAPI_MultipleWallets(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	request := types.BalanceRequest{
		Wallets: testWallets,
	}

	reqBody, _ := json.Marshal(request)
	req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", ts.testAPIKey)

	t.Logf("Testing multiple wallets: %d addresses", len(testWallets))

	start := time.Now()
	resp, err := ts.app.Test(req, 30000)
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

func TestAPI_SameWalletMultipleRequests(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	wallet := testWallets[0]
	numRequests := 5

	request := types.BalanceRequest{
		Wallets: []string{wallet},
	}
	reqBody, _ := json.Marshal(request)

	t.Logf("Testing %d sequential requests for wallet: %s", numRequests, wallet)

	var balances []float64
	var durations []time.Duration

	for i := 0; i < numRequests; i++ {
		req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", ts.testAPIKey)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.1.%d", 10+i))

		start := time.Now()
		resp, err := ts.app.Test(req, 30000)
		duration := time.Since(start)

		require.NoError(t, err)

		if resp.StatusCode != fiber.StatusOK {
			t.Logf("Request %d failed with status %d", i+1, resp.StatusCode)
			continue
		}

		var response types.BalanceResponse
		body, _ := io.ReadAll(resp.Body)
		err = json.Unmarshal(body, &response)

		require.NoError(t, err)
		assert.True(t, response.Success)
		assert.Len(t, response.Data, 1)

		balance := response.Data[0].Balance
		balances = append(balances, balance)
		durations = append(durations, duration)

		t.Logf("Request %d: %f SOL (took %v)", i+1, balance, duration)
	}

	require.GreaterOrEqual(t, len(balances), 2, "Need at least 2 successful requests")

	for i := 1; i < len(balances); i++ {
		assert.Equal(t, balances[0], balances[i], "All requests should return same balance")
	}

	if len(durations) >= 2 {
		assert.True(t, durations[1] < durations[0], "Second request should be faster (cached)")
	}

	t.Logf("✓ Same wallet multiple requests test passed - Cache working properly")
}

func TestAPI_ConcurrentMixedRequests(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	t.Log("Testing concurrent mixed requests (single + multiple + repeated)")

	var wg sync.WaitGroup
	results := make(chan string, 10)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			request := types.BalanceRequest{
				Wallets: []string{testWallets[index%len(testWallets)]},
			}
			reqBody, _ := json.Marshal(request)

			req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", ts.testAPIKey)
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.1.%d", 20+index))

			start := time.Now()
			resp, err := ts.app.Test(req, 30000)
			duration := time.Since(start)

			if err == nil && resp.StatusCode == fiber.StatusOK {
				results <- "Single wallet request " + string(rune('A'+index)) + " succeeded in " + duration.String()
			} else {
				results <- "Single wallet request " + string(rune('A'+index)) + " failed"
			}
		}(i)
	}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			request := types.BalanceRequest{
				Wallets: testWallets,
			}
			reqBody, _ := json.Marshal(request)

			req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", ts.testAPIKey)
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.1.%d", 30+index))

			start := time.Now()
			resp, err := ts.app.Test(req, 30000)
			duration := time.Since(start)

			if err == nil && resp.StatusCode == fiber.StatusOK {
				results <- "Multiple wallet request " + string(rune('1'+index)) + " succeeded in " + duration.String()
			} else {
				results <- "Multiple wallet request " + string(rune('1'+index)) + " failed"
			}
		}(i)
	}

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			request := types.BalanceRequest{
				Wallets: []string{testWallets[0]},
			}
			reqBody, _ := json.Marshal(request)

			req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", ts.testAPIKey)
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.1.%d", 40+index))

			start := time.Now()
			resp, err := ts.app.Test(req, 30000)
			duration := time.Since(start)

			if err == nil && resp.StatusCode == fiber.StatusOK {
				results <- "Repeated wallet request " + string(rune('X'+index)) + " succeeded in " + duration.String()
			} else {
				results <- "Repeated wallet request " + string(rune('X'+index)) + " failed"
			}
		}(i)
	}

	wg.Wait()
	close(results)

	successCount := 0
	for result := range results {
		t.Log(result)
		if len(result) > 6 && result[len(result)-6:] != "failed" {
			successCount++
		}
	}

	assert.GreaterOrEqual(t, successCount, 1, "At least some concurrent requests should succeed")
	t.Logf("✓ Concurrent mixed requests test passed - %d/8 requests succeeded", successCount)
}

func TestAPI_RateLimiting(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}
	reqBody, _ := json.Marshal(request)

	t.Log("Testing IP rate limiting (10 requests per minute limit)")

	clientIP := "192.168.1.100"
	successCount := 0
	rateLimitedCount := 0

	for i := 0; i < 12; i++ {
		req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", ts.testAPIKey)
		req.Header.Set("X-Forwarded-For", clientIP)

		resp, err := ts.app.Test(req, 30000)
		require.NoError(t, err)

		if resp.StatusCode == fiber.StatusOK {
			successCount++
			t.Logf("Request %d: Success", i+1)
		} else if resp.StatusCode == fiber.StatusTooManyRequests {
			rateLimitedCount++
			t.Logf("Request %d: Rate limited", i+1)
		} else {
			t.Logf("Request %d: Unexpected status %d", i+1, resp.StatusCode)
		}
	}

	t.Logf("Rate limiting results: %d successful, %d rate limited", successCount, rateLimitedCount)

	assert.GreaterOrEqual(t, successCount, 1, "Should allow at least 1 request")
	assert.GreaterOrEqual(t, rateLimitedCount, 1, "Should rate limit some requests")

	t.Log("✓ Rate limiting test passed")
}

func TestAPI_Caching(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	wallet := testWallets[0]
	request := types.BalanceRequest{
		Wallets: []string{wallet},
	}
	reqBody, _ := json.Marshal(request)

	t.Logf("Testing caching behavior for wallet: %s", wallet)

	req1, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("X-API-Key", ts.testAPIKey)
	req1.Header.Set("X-Forwarded-For", "192.168.1.50")

	t.Log("Making first request (cache miss)...")
	start1 := time.Now()
	resp1, err := ts.app.Test(req1, 30000)
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
	req2.Header.Set("X-API-Key", ts.testAPIKey)
	req2.Header.Set("X-Forwarded-For", "192.168.1.51")

	t.Log("Making second request (cache hit)...")
	start2 := time.Now()
	resp2, err := ts.app.Test(req2, 30000)
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

func TestAPI_AuthenticationAndRateLimiting(t *testing.T) {
	ts := setupTest(t)
	defer ts.cleanup(t)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}
	reqBody, _ := json.Marshal(request)

	t.Log("Testing authentication and rate limiting together")

	req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := ts.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	t.Log("✓ No API key: Correctly rejected")

	req, _ = http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "invalid-key")

	resp, err = ts.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	t.Log("✓ Invalid API key: Correctly rejected")

	clientIP := "192.168.1.200"

	successCount := 0
	rateLimitedCount := 0
	for i := 0; i < 15; i++ {
		req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", ts.testAPIKey)
		req.Header.Set("X-Forwarded-For", clientIP)

		resp, _ := ts.app.Test(req, 30000)
		if resp.StatusCode == fiber.StatusOK {
			successCount++
		} else if resp.StatusCode == fiber.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	assert.GreaterOrEqual(t, successCount, 1, "Should allow at least 1 request")
	assert.GreaterOrEqual(t, rateLimitedCount, 1, "Should rate limit some requests")

	t.Log("✓ Valid API key with rate limiting: Working correctly")

	req, _ = http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", ts.testAPIKey)
	req.Header.Set("X-Forwarded-For", "192.168.1.201")

	resp, err = ts.app.Test(req, 30000)
	require.NoError(t, err)

	if resp.StatusCode == fiber.StatusOK {
		t.Log("✓ Valid API key from different IP: Working correctly")
	} else if resp.StatusCode == fiber.StatusTooManyRequests {
		t.Log("✓ Valid API key from different IP: Rate limited (acceptable)")
	} else {
		t.Logf("Unexpected status code: %d", resp.StatusCode)
	}
	t.Log("✓ Authentication and rate limiting test passed")
}
