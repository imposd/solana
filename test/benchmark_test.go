package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
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

func BenchmarkAPI_SingleWallet(b *testing.B) {
	ts := setupBenchmark(b)
	defer ts.cleanupBenchmark(b)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}
	reqBody, _ := json.Marshal(request)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", ts.testAPIKey)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.1.%d", 100+(i%200)))

		resp, err := ts.app.Test(req, 30000)
		if err != nil || resp.StatusCode != 200 {
			b.Errorf("Request failed: %v, status: %d", err, resp.StatusCode)
		}
	}
}

func BenchmarkAPI_MultipleWallets(b *testing.B) {
	ts := setupBenchmark(b)
	defer ts.cleanupBenchmark(b)

	request := types.BalanceRequest{
		Wallets: testWallets,
	}
	reqBody, _ := json.Marshal(request)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", ts.testAPIKey)
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.2.%d", 100+(i%200)))

		resp, err := ts.app.Test(req, 30000)
		if err != nil || resp.StatusCode != 200 {
			b.Errorf("Request failed: %v, status: %d", err, resp.StatusCode)
		}
	}
}

func BenchmarkAPI_CachePerformance(b *testing.B) {
	ts := setupBenchmark(b)
	defer ts.cleanupBenchmark(b)

	address := testWallets[0]

	b.Run("CacheMiss", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {

			fakeAddress := "1111111111111111111111111111111" + string(rune('0'+i%10))
			_, _ = ts.solanaService.GetBalance(fakeAddress)
		}
	})

	b.Run("CacheHit", func(b *testing.B) {

		ts.solanaService.GetBalance(address)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = ts.solanaService.GetBalance(address)
		}
	})
}

func BenchmarkAPI_ConcurrentRequests(b *testing.B) {
	ts := setupBenchmark(b)
	defer ts.cleanupBenchmark(b)

	request := types.BalanceRequest{
		Wallets: []string{testWallets[0]},
	}
	reqBody, _ := json.Marshal(request)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req, _ := http.NewRequest("POST", "/api/get-balance", bytes.NewReader(reqBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-API-Key", ts.testAPIKey)
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("192.168.3.%d", 1+(i%254)))

			_, err := ts.app.Test(req, 30000)
			if err != nil {
				b.Errorf("Request failed: %v", err)
			}
			i++
		}
	})
}

func setupBenchmark(b *testing.B) *TestSuite {
	b.Helper()

	middleware.ClearRateLimiters()

	if err := godotenv.Load("../.env"); err != nil {
		b.Logf("Warning: Could not load .env file: %v", err)
	}

	if os.Getenv("HELIUS_API_KEY") == "" {
		b.Skip("HELIUS_API_KEY not set")
	}

	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		b.Skipf("MongoDB not available: %v", err)
	}

	if err := mongoClient.Ping(ctx, nil); err != nil {
		b.Skipf("MongoDB not responding: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr: cfg.RedisURI,
	})

	if err := redisClient.Ping(ctx).Err(); err != nil {
		b.Skipf("Redis not available: %v", err)
	}

	solanaService := services.NewSolanaService(cfg.SolaanRPCURL, redisClient)

	testAPIKey := "benchmark-key-123"
	collection := mongoClient.Database("nova").Collection("api_keys")

	collection.DeleteOne(ctx, bson.M{"key": testAPIKey})

	apiKey := types.APIKey{
		Key:       testAPIKey,
		Active:    true,
		CreatedAt: time.Now(),
	}

	db := &types.Database{
		MongoDB: mongoClient,
		Redis:   redisClient,
	}

	_, err = collection.InsertOne(ctx, apiKey)
	require.NoError(b, err, "API key insertion should succeed")

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

func (ts *TestSuite) cleanupBenchmark(b *testing.B) {
	b.Helper()

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
