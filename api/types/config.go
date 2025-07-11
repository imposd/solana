package types

import "time"

type Config struct {
	Port         string
	MongoURI     string
	RedisURI     string
	SolaanRPCURL string
	CacheTTL     time.Duration
	RateLimit    int
}
