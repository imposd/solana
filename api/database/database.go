package database

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"nova/api/types"
)

func New(cfg *types.Config) *types.Database {
	db := &types.Database{}
	initMongoDB(db, cfg)
	initRedis(db, cfg)
	return db
}

func initMongoDB(db *types.Database, cfg *types.Config) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().
		ApplyURI(cfg.MongoURI).
		SetMaxPoolSize(100).
		SetMinPoolSize(10).
		SetMaxConnIdleTime(30 * time.Second).
		SetMaxConnecting(20).
		SetConnectTimeout(5 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetHeartbeatInterval(10 * time.Second)

	var err error
	db.MongoDB, err = mongo.Connect(clientOptions)
	if err != nil {
		log.Fatal("Error connecting to MongoDB:", err)
		return
	}

	err = db.MongoDB.Ping(ctx, readpref.Primary())
	if err != nil {
		log.Fatal("Error pinging MongoDB:", err)
		return
	}
}

func initRedis(db *types.Database, cfg *types.Config) {
	addr := cfg.RedisURI
	if after, ok := strings.CutPrefix(addr, "redis://"); ok {
		addr = after
	}

	db.Redis = redis.NewClient(&redis.Options{
		Addr: addr,
	})

	if err := db.Redis.Ping(context.Background()).Err(); err != nil {
		log.Fatal("Error connecting to Redis:", err)
		return
	}
}
