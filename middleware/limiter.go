package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type RateLimiter struct {
	store  map[string]*[]int64
	mu     sync.Mutex
	expire time.Duration
}

func (l *RateLimiter) Init(expire time.Duration) {
	if l.store == nil {
		l.mu.Lock()
		if l.store == nil {
			l.store = make(map[string]*[]int64)
			l.expire = expire
			if expire > 0 {
				go l.clearExpiredItems()
			}
		}
		l.mu.Unlock()
	}
}

func (l *RateLimiter) clearExpiredItems() {
	for {
		time.Sleep(l.expire)
		l.mu.Lock()
		now := time.Now().Unix()
		for key := range l.store {
			queue := l.store[key]
			size := len(*queue)
			if size == 0 || now-(*queue)[size-1] > int64(l.expire.Seconds()) {
				delete(l.store, key)
			}
		}
		l.mu.Unlock()
	}
}

// Request parameter duration's unit is seconds
func (l *RateLimiter) Request(key string, maxRequestNum int, duration int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	// [old <-- new]
	queue, ok := l.store[key]
	now := time.Now().Unix()
	if ok {
		if len(*queue) < maxRequestNum {
			*queue = append(*queue, now)
			return true
		} else {
			if now-(*queue)[0] >= duration {
				*queue = (*queue)[1:]
				*queue = append(*queue, now)
				return true
			} else {
				return false
			}
		}
	} else {
		s := make([]int64, 0, maxRequestNum)
		l.store[key] = &s
		*(l.store[key]) = append(*(l.store[key]), now)
	}
	return true
}

// keyedRateLimiter budgets requests per key. A non-positive budget disables it.
func keyedRateLimiter(maxRequestNum int, duration int64, key func(c *gin.Context) string) gin.HandlerFunc {
	if maxRequestNum <= 0 {
		return func(c *gin.Context) { c.Next() }
	}
	rl := &RateLimiter{}
	rl.Init(time.Duration(duration) * time.Second)

	return func(c *gin.Context) {
		if !rl.Request(key(c), maxRequestNum, duration) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    "rate_limited",
				"message": "Too many requests",
			})
			return
		}
		c.Next()
	}
}

// IpRateLimiter budgets unauthenticated traffic per client IP.
func IpRateLimiter(maxRequestNum int, duration int64) gin.HandlerFunc {
	return keyedRateLimiter(maxRequestNum, duration, func(c *gin.Context) string {
		return "ip:" + c.ClientIP()
	})
}

// OwnerRateLimiter budgets authenticated traffic per Owner. Every protected
// request arrives from the frontend BFF process, so all Owners share one client
// IP and a per-IP budget would throttle the whole deployment. Register it after
// ValidateJWT; without an Owner in context it falls back to the client IP.
func OwnerRateLimiter(maxRequestNum int, duration int64) gin.HandlerFunc {
	return keyedRateLimiter(maxRequestNum, duration, func(c *gin.Context) string {
		if ownerID := c.GetUint("user_id"); ownerID != 0 {
			return "owner:" + strconv.FormatUint(uint64(ownerID), 10)
		}
		return "ip:" + c.ClientIP()
	})
}
