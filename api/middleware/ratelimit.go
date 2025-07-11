package middleware

import (
	"sync"

	"github.com/gofiber/fiber/v2"
	"golang.org/x/time/rate"

	"nova/api/types"
)

var (
	rateLimiters sync.Map
)

func RateLimitMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		clientIP := c.IP()

		limiterInterface, _ := rateLimiters.LoadOrStore(clientIP, rate.NewLimiter(rate.Limit(10.0/60.0), 9))
		limiter := limiterInterface.(*rate.Limiter)

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
	rateLimiters.Range(func(key, value interface{}) bool {
		rateLimiters.Delete(key)
		return true
	})
}
