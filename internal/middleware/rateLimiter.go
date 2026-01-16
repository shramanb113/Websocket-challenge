package middleware

import (
	"sync/atomic"
	"time"
)

const (
	burstLimit = 5
	refillRate = 500 * time.Millisecond
)

type RateLimiter struct {
	token    int32
	rate     time.Duration
	burst    int32
	lastTick int64
}

func NewRatelimiter(token int32, rate time.Duration) *RateLimiter {
	return &RateLimiter{
		token:    token,
		rate:     rate,
		lastTick: time.Now().Unix(),
		burst:    burstLimit,
	}
}

func (l *RateLimiter) Allow() bool {
	now := time.Now().Unix()

	last := atomic.LoadInt64(&l.lastTick)

	elapsed := now - last

	generated := int32(elapsed / int64(l.rate))

	if generated > 0 {
		if atomic.CompareAndSwapInt64(&l.lastTick, last, now) {
			current := atomic.LoadInt32(&l.token)
			newBalance := current + generated

			if newBalance > l.burst {
				newBalance = l.burst
			}
			atomic.StoreInt32(&l.token, newBalance)
		}
	}

	for {
		current := atomic.LoadInt32(&l.token)

		if current <= 0 {
			return false
		}
		if current > 0 {
			atomic.CompareAndSwapInt32(&l.token, current, current-1)
			return true
		}
	}

}
