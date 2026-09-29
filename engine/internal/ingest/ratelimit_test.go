package ingest

import (
	"testing"
	"time"
)

type fakeClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
}

func totalSleep(c *fakeClock) time.Duration {
	var sum time.Duration
	for _, d := range c.sleeps {
		sum += d
	}
	return sum
}

func TestRateLimiter(t *testing.T) {
	t.Run("first request does not sleep", func(t *testing.T) {
		c := &fakeClock{now: time.Unix(0, 0)}
		rl := newRateLimiter(200, 7, c)
		rl.Wait()
		if len(c.sleeps) != 0 {
			t.Errorf("sleeps = %v, want none", c.sleeps)
		}
	})
	t.Run("spacing enforced between requests", func(t *testing.T) {
		c := &fakeClock{now: time.Unix(0, 0)}
		rl := newRateLimiter(200, 7, c)
		rl.Wait()
		rl.Wait()
		rl.Wait()
		if len(c.sleeps) != 2 {
			t.Fatalf("sleeps = %v, want 2", c.sleeps)
		}
		for i, d := range c.sleeps {
			if d != 200*time.Millisecond {
				t.Errorf("sleeps[%d] = %v, want 200ms", i, d)
			}
		}
	})
	t.Run("burst cap delays when exhausted", func(t *testing.T) {
		c := &fakeClock{now: time.Unix(0, 0)}
		rl := newRateLimiter(1, 1, c)
		rl.Wait()
		if len(c.sleeps) != 0 {
			t.Fatalf("sleeps = %v, want none", c.sleeps)
		}
		rl.Wait()
		if got := totalSleep(c); got < 990*time.Millisecond {
			t.Errorf("total sleep %v, want >= 990ms for a 1 token/s bucket", got)
		}
	})
	t.Run("idle time refills the bucket", func(t *testing.T) {
		c := &fakeClock{now: time.Unix(0, 0)}
		rl := newRateLimiter(200, 7, c)
		rl.Wait()
		c.now = c.now.Add(10 * time.Second)
		rl.Wait()
		if len(c.sleeps) != 0 {
			t.Errorf("sleeps = %v, want none after idle refill", c.sleeps)
		}
	})
}
