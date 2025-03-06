package middleware

import (
	"net"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimiter struct {
	mu           sync.Mutex
	requestCount map[string]int
	limit        int
	window       time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requestCount: make(map[string]int),
		limit:        limit,
		window:       window,
	}

	// Periodically clean up old entries
	go func() {
		for {
			time.Sleep(window)
			rl.mu.Lock()
			rl.requestCount = make(map[string]int)
			rl.mu.Unlock()
		}
	}()

	return rl
}

func (rl *RateLimiter) Limit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get client IP
		ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			ip = c.ClientIP()
		}

		rl.mu.Lock()
		defer rl.mu.Unlock()

		// Increment request count for this IP
		rl.requestCount[ip]++

		// Check if request count exceeds limit
		if rl.requestCount[ip] > rl.limit {
			c.JSON(429, gin.H{
				"error":   "Too Many Requests",
				"message": "Rate limit exceeded. Please wait before making more requests.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}

// Global rate limiter instances for different endpoints
var (
	GlobalRateLimiter = NewRateLimiter(100, 1*time.Minute) // 100 requests per minute
	StrictRateLimiter = NewRateLimiter(10, 1*time.Minute)  // 10 requests per minute for sensitive endpoints
)
