package types

import (
	"time"
)

type APIKey struct {
	Key       string    `bson:"key"`
	Active    bool      `bson:"active"`
	CreatedAt time.Time `bson:"created_at"`
}

type CacheEntry struct {
	Balance   float64
	Timestamp time.Time
}
