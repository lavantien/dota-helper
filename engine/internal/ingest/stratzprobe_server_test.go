package ingest

import (
	"os"
	"strings"
	"testing"

	"poolguide/internal/config"
)

// ProbeStratz walks the pool under the acceptance gates: missing roster slugs
// are skipped, the probe caps at three heroes, and every downstream failure
// is loud
func TestProbeStratzAgainstStubServer(t *testing.T) {
	pool := []config.PoolEntry{{Slug: "pudge"}, {Slug: "axe"}, {Slug: "anti-mage"}, {Slug: "dark-seer"}}

	t.Run("probes pool heroes found in the roster", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, nil)
		cfg.Pool = pool[:3] // axe is not in the roster, anti-mage + pudge are
		if err := ProbeStratz(cfg); err != nil {
			t.Fatal(err)
		}
		if s.count() != 4 {
			t.Fatalf("probe made %d requests, want 4 (roster + 2 matchups + popularity)", s.count())
		}
	})

	t.Run("caps probing at three heroes", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "constants { heroes") {
				return dataReply(crawlRosterFour), true
			}
			return stubReply{}, false
		})
		cfg.Pool = pool
		if err := ProbeStratz(cfg); err != nil {
			t.Fatal(err)
		}
		if n := s.countMatching("matchUp("); n != 3 {
			t.Fatalf("matchUp probed %d heroes, want the cap of 3", n)
		}
	})

	t.Run("thin hero fails acceptance without failing the probe", func(t *testing.T) {
		cfg, s := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "matchUp(heroId: 8") {
				return dataReply(matchUpEnvelope(8, "", "")), true
			}
			return stubReply{}, false
		})
		cfg.Pool = []config.PoolEntry{{Slug: "dark-seer"}}
		if err := ProbeStratz(cfg); err != nil {
			t.Fatal(err)
		}
		s.saw(t, "matchUp(heroId: 8", "thin hero probe")
	})

	t.Run("missing token", func(t *testing.T) {
		cfg, _ := crawlTestEnv(t, nil)
		t.Setenv("STRATZ_TOKEN", "")
		os.Remove(cfg.Paths.TokenFile)
		if err := ProbeStratz(cfg); err == nil || !strings.Contains(err.Error(), "token missing") {
			t.Fatalf("err = %v, want missing token", err)
		}
	})

	t.Run("roster failure names the token file", func(t *testing.T) {
		cfg, _ := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "constants { heroes") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		cfg.Pool = pool[:3]
		err := ProbeStratz(cfg)
		if err == nil || !strings.Contains(err.Error(), "stratz probe roster") || !strings.Contains(err.Error(), cfg.Paths.TokenFile) {
			t.Fatalf("err = %v, want the roster failure naming the token file", err)
		}
	})

	t.Run("matchup failure fails the probe", func(t *testing.T) {
		cfg, _ := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "matchUp(") {
				return stubReply{code: 403, body: "denied"}, true
			}
			return stubReply{}, false
		})
		cfg.Pool = []config.PoolEntry{{Slug: "anti-mage"}}
		if err := ProbeStratz(cfg); err == nil || !strings.Contains(err.Error(), "stratz probe anti-mage") {
			t.Fatalf("err = %v, want the per-hero probe failure", err)
		}
	})

	t.Run("empty popularity fails the probe tail", func(t *testing.T) {
		cfg, _ := crawlTestEnv(t, func(q string) (stubReply, bool) {
			if strings.Contains(q, "winDay(") {
				return dataReply(`{"heroStats":{"winDay":[]}}`), true
			}
			return stubReply{}, false
		})
		cfg.Pool = []config.PoolEntry{{Slug: "anti-mage"}}
		if err := ProbeStratz(cfg); err == nil || !strings.Contains(err.Error(), "answered no rows") {
			t.Fatalf("err = %v, want the empty aggregate failure", err)
		}
	})
}
