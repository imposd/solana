package test

import (
	"nova/api/services"
	"nova/api/types"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

var testWallets = []string{
	"7xLk17EQQ5KLDLDe44wCmupJKJjTGd8hs3eSVVhCx932",
	"autistHRRqeEmDp92E81uqZqbpEfSKAdC4EbfD84AzE",
	"Ag3Gao5hvTPDsHLBf5SBDse8wQwBrMcgE6ox1GoKgTuh",
}

type TestSuite struct {
	app           *fiber.App
	mongoClient   *mongo.Client
	redisClient   *redis.Client
	solanaService *services.SolanaService
	testAPIKey    string
	cfg           *types.Config
}
