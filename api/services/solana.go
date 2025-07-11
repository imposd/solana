package services

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/redis/go-redis/v9"

	"nova/api/types"
)

type SolanaService struct {
	client       *rpc.Client
	redisClient  *redis.Client
	cache        map[string]*types.CacheEntry
	cacheMutex   *sync.RWMutex
	requestMutex map[string]*sync.Mutex
	mutexMapLock *sync.Mutex
}

func NewSolanaService(rpcURL string, redisClient *redis.Client) *SolanaService {
	return &SolanaService{
		client:       rpc.New(rpcURL),
		redisClient:  redisClient,
		cache:        make(map[string]*types.CacheEntry),
		cacheMutex:   &sync.RWMutex{},
		requestMutex: make(map[string]*sync.Mutex),
		mutexMapLock: &sync.Mutex{},
	}
}

func (s *SolanaService) GetBalance(address string) (float64, error) {
	s.mutexMapLock.Lock()

	if _, exists := s.requestMutex[address]; !exists {
		s.requestMutex[address] = &sync.Mutex{}
	}

	mutex := s.requestMutex[address]
	s.mutexMapLock.Unlock()

	mutex.Lock()
	defer mutex.Unlock()

	if cachedBalance, valid := s.getCachedBalance(address); valid {
		return cachedBalance, nil
	}

	balance, err := s.fetchSolanaBalance(address)

	if err != nil {
		return 0, err
	}

	s.setCachedBalance(address, balance)

	return balance, nil
}

func (s *SolanaService) getCachedBalance(address string) (float64, bool) {
	s.cacheMutex.RLock()

	defer s.cacheMutex.RUnlock()

	if entry, exists := s.cache[address]; exists {
		if time.Since(entry.Timestamp) < 10*time.Second {
			return entry.Balance, true
		}
	}

	return 0, false
}

func (s *SolanaService) setCachedBalance(address string, balance float64) {
	s.cacheMutex.Lock()

	defer s.cacheMutex.Unlock()

	s.cache[address] = &types.CacheEntry{
		Balance:   balance,
		Timestamp: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	s.redisClient.Set(ctx, fmt.Sprintf("balance:%s", address), balance, 10*time.Second)
}

func (s *SolanaService) fetchSolanaBalance(address string) (float64, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return 0, fmt.Errorf("empty wallet address")
	}

	pubKey, err := solana.PublicKeyFromBase58(address)

	if err != nil {
		return 0, fmt.Errorf("invalid wallet address format: %s", address)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	out, err := s.client.GetBalance(ctx, pubKey, rpc.CommitmentFinalized)

	if err != nil {
		return 0, fmt.Errorf("failed to get balance for %s: %v", address, err)
	}

	lamportsOnAccount := new(big.Float).SetUint64(uint64(out.Value))
	solBalance := new(big.Float).Quo(lamportsOnAccount, new(big.Float).SetUint64(solana.LAMPORTS_PER_SOL))

	balance, _ := solBalance.Float64()

	return balance, nil
}

func (s *SolanaService) GetMultipleBalances(addresses []string) []types.WalletBalance {
	var wg sync.WaitGroup
	results := make([]types.WalletBalance, len(addresses))

	for i, address := range addresses {
		wg.Add(1)
		go func(index int, addr string) {
			defer wg.Done()
			balance, err := s.GetBalance(addr)
			if err != nil {
				results[index] = types.WalletBalance{
					Address: addr,
					Balance: 0,
					Error:   err.Error(),
				}
			} else {
				results[index] = types.WalletBalance{
					Address: addr,
					Balance: balance,
				}
			}
		}(i, address)
	}

	wg.Wait()

	return results
}
