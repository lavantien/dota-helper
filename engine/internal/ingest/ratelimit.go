package ingest

import "time"

// clock is injectable so limiter tests run without real waiting.
type clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// rateLimiter combines a min spacing between requests with a token bucket:
// stratz enforces both, and the bucket takes over once the per-second burst
// allowance is spent.
type rateLimiter struct {
	c          clock
	interval   time.Duration
	rate       float64 // tokens per second, also the bucket capacity
	tokens     float64
	lastReq    time.Time
	lastRefill time.Time
}

func newRateLimiter(intervalMs, burstPerSec int, c clock) *rateLimiter {
	now := c.Now()
	return &rateLimiter{
		c:          c,
		interval:   time.Duration(intervalMs) * time.Millisecond,
		rate:       float64(burstPerSec),
		tokens:     float64(burstPerSec),
		lastRefill: now,
	}
}

// Wait blocks until the next request is allowed.
func (r *rateLimiter) Wait() {
	now := r.c.Now()
	var wait time.Duration
	if !r.lastReq.IsZero() {
		if need := r.interval - now.Sub(r.lastReq); need > 0 {
			wait = need
		}
	}
	r.refill(now)
	if r.tokens < 1 {
		if need := time.Duration((1 - r.tokens) / r.rate * float64(time.Second)); need > wait {
			wait = need
		}
	}
	if wait > 0 {
		r.c.Sleep(wait)
		now = r.c.Now()
		r.refill(now)
	}
	r.tokens--
	r.lastReq = now
}

func (r *rateLimiter) refill(now time.Time) {
	r.tokens += now.Sub(r.lastRefill).Seconds() * r.rate
	if r.tokens > r.rate {
		r.tokens = r.rate
	}
	r.lastRefill = now
}
