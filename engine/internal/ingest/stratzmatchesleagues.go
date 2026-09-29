package ingest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"poolguide/internal/config"
)

type leagueEntry struct {
	ID            int64  `json:"id"`
	DisplayName   string `json:"displayName"`
	LastMatchDate int64  `json:"lastMatchDate"`
}

type leaguesFile struct {
	FetchedAt string       `json:"fetchedAt"`
	Leagues   []leagueEntry `json:"leagues"`
}

func leaguesCachePath(cfg *config.Config) string {
	return filepath.Join(cfg.Paths.MatchesRawDir, "_leagues.json")
}

// fetchLeaguesCache loads the cached league list or pages it once. The
// listing arrives ordered by lastMatchDate descending, so it stops at the
// first fully-dead page (no league active since the patch epoch) instead of
// walking thousands of historical leagues. The return counts the live
// listing calls so the manifest budget stays honest (0 when fully cached).
func fetchLeaguesCache(c gqlFetcher, cfg *config.Config, since int64) ([]leagueEntry, int, error) {
	if b, err := os.ReadFile(leaguesCachePath(cfg)); err == nil {
		var lf leaguesFile
		if json.Unmarshal(b, &lf) == nil && len(lf.Leagues) > 0 {
			return lf.Leagues, 0, nil
		}
	}
	take := clampTake(cfg.Backfill.Take)
	var all []leagueEntry
	calls := 0
	for skip := 0; ; skip += take {
		body, err := c.query(leaguesPageQuery(take, skip))
		if err != nil {
			return nil, calls, fmt.Errorf("leagues page %d: %w", skip, err)
		}
		calls++
		var d struct {
			Leagues []leagueEntry `json:"leagues"`
		}
		if err := decodeEnvelope(body, &d); err != nil {
			return nil, calls, err
		}
		all = append(all, d.Leagues...)
		if len(d.Leagues) > 0 {
			fmt.Printf("matches backfill: leagues listing page %d served %d, newest lastMatchDate %d\n",
				skip, len(d.Leagues), d.Leagues[len(d.Leagues)-1].LastMatchDate)
		}
		if len(d.Leagues) < take {
			break
		}
		if !pageAlive(d.Leagues, since) {
			fmt.Printf("matches backfill: leagues listing hit the dead zone at page %d, stopping\n", skip)
			break
		}
	}
	if err := writeJson(leaguesCachePath(cfg), &leaguesFile{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Leagues:   all,
	}); err != nil {
		return nil, calls, err
	}
	return all, calls, nil
}

// pageAlive reports whether any league on a listing page was active since
// the patch epoch.
func pageAlive(leagues []leagueEntry, since int64) bool {
	for _, lg := range leagues {
		if lg.LastMatchDate >= since {
			return true
		}
	}
	return false
}
