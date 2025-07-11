package middleware

import (
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/time/rate"

	"nova/api/types"
)

var (
	rateLimiters = make(map[string]*rate.Limiter)
	rateMutex    = &sync.RWMutex{}
)

func RateLimitMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		clientIP := c.IP()

		rateMutex.RLock()
		limiter, exists := rateLimiters[clientIP]
		rateMutex.RUnlock()

		if !exists {
			rateMutex.Lock()
			if limiter, exists = rateLimiters[clientIP]; !exists {
				limiter = rate.NewLimiter(rate.Every(time.Minute/10), 10)
				rateLimiters[clientIP] = limiter
			}
			rateMutex.Unlock()
		}

		if !limiter.Allow() {
			return c.Status(fiber.StatusTooManyRequests).JSON(types.ErrorResponse{
				Success: false,
				Message: "Ratelimit exceeded: 10 requests per minute",
			})
		}

		return c.Next()
	}
}

func ClearRateLimiters() {
	rateMutex.Lock()
	rateLimiters = make(map[string]*rate.Limiter)
	rateMutex.Unlock()
}
