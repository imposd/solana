package types

import (
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type Database struct {
	MongoDB *mongo.Client
	Redis   *redis.Client
}
