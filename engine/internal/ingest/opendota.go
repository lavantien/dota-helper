package ingest

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"poolguide/internal/config"
)

const opendotaBase = "https://api.opendota.com"

type explorerRow struct {
	Kind     string `json:"kind"`
	OtherID  int    `json:"other_id"`
	Matches  int64  `json:"matches"`
	PoolWins int64  `json:"pool_wins"`
}

// buildExplorerSQL pairs one pool hero against every teammate and opponent in
// the same matches since the patch epoch. Argument order is pinned by the
// golden test; gameMode and lobbyType come from the scope hub keys.
func buildExplorerSQL(poolID, gameMode, lobbyType, rankTierMin int, startUnix int64) string {
	return fmt.Sprintf(
		"SELECT CASE WHEN (pa.player_slot < 128) = (pb.player_slot < 128) THEN 'ally' ELSE 'enemy' END AS kind, "+
			"pb.hero_id AS other_id, count(*) AS matches, "+
			"sum(CASE WHEN (pa.player_slot < 128) = m.radiant_win THEN 1 ELSE 0 END) AS pool_wins "+
			"FROM player_matches pa JOIN player_matches pb ON pb.match_id = pa.match_id AND pb.hero_id <> %d "+
			"JOIN matches m ON m.match_id = pa.match_id "+
			"WHERE pa.hero_id = %d AND pa.start_time >= %d "+
			"AND pa.lobby_type = %d AND pa.game_mode = %d AND pa.rank_tier >= %d "+
			"GROUP BY 1, 2",
		poolID, poolID, startUnix, lobbyType, gameMode, rankTierMin)
}

// patchEpochUnix converts the hub's YYYY-MM-DD epoch to seconds. Unparsable
// input returns 0; FetchOpenDota validates the format before use.
func patchEpochUnix(epoch string) int64 {
	t, err := time.Parse("2006-01-02", epoch)
	if err != nil {
		return 0
	}
	return t.UTC().Unix()
}

// parseExplorerResponse keeps rows at or above the min match count.
func parseExplorerResponse(b []byte, minMatches int) ([]explorerRow, error) {
	var resp struct {
		Rows []explorerRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	out := make([]explorerRow, 0, len(resp.Rows))
	for _, r := range resp.Rows {
		if r.Matches >= int64(minMatches) {
			out = append(out, r)
		}
	}
	return out, nil
}

type opendotaClient struct {
	http *http.Client
	rl   *rateLimiter
}

func newOpenDotaClient(cfg *config.Config) *opendotaClient {
	return &opendotaClient{
		http: &http.Client{Timeout: httpTimeout},
		rl:   newRateLimiter(60000/cfg.OpenDota.RequestsPerMin, cfg.OpenDota.RequestsPerMin, realClock{}),
	}
}

// get performs one rate-limited GET and returns the body with its status so
// callers can treat outages as data instead of errors.
func (o *opendotaClient) get(path string) ([]byte, int, error) {
	o.rl.Wait()
	req, err := http.NewRequest(http.MethodGet, opendotaBase+path, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := o.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// ProbeOpenDota reports api health. A clean down report is a pass: the api
// has known outage windows and the pipeline falls back to other tiers.
func ProbeOpenDota(cfg *config.Config) error {
	c := newOpenDotaClient(cfg)
	body, status, err := c.get("/api/heroes")
	if err != nil || status != http.StatusOK {
		fmt.Printf("opendota: DOWN (status %d, %v) - a clean down report is a probe pass\n", status, err)
		return nil
	}
	var heroes []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(body, &heroes); err != nil {
		return fmt.Errorf("opendota heroes parse: %w", err)
	}
	fmt.Printf("opendota: UP, %d heroes listed\n", len(heroes))
	probeOpenDotaExplorer(c, cfg)
	return nil
}

// probeOpenDotaExplorer runs one light explorer query for the first pool hero
// when the stratz roster cache is present. Failures print, never fatal.
func probeOpenDotaExplorer(c *opendotaClient, cfg *config.Config) {
	rf, err := readRosterFile(cfg)
	if err != nil || len(rf.Heroes) == 0 {
		fmt.Println("opendota explorer: skipped, no roster cache (run fetch stratz first)")
		return
	}
	slug := cfg.PoolSlugs()[0]
	id := 0
	for _, h := range rf.Heroes {
		if h.Slug == slug {
			id = h.ID
			break
		}
	}
	if id == 0 {
		fmt.Printf("opendota explorer: skipped, %s not in roster cache\n", slug)
		return
	}
	query := buildExplorerSQL(id, cfg.Scope.GameMode, cfg.Scope.LobbyType, cfg.Scope.Explorer.RankTierMin, patchEpochUnix(cfg.PatchEpoch))
	body, status, err := c.get("/api/explorer?sql=" + url.QueryEscape(query))
	if err != nil || status != http.StatusOK {
		fmt.Printf("opendota explorer: DOWN for queries (status %d, %v)\n", status, err)
		return
	}
	rows, err := parseExplorerResponse(body, cfg.OpenDota.ExplorerMinMatches)
	if err != nil {
		fmt.Printf("opendota explorer parse: %v\n", err)
		return
	}
	ally, enemy := 0, 0
	for _, r := range rows {
		if r.Kind == "ally" {
			ally++
		} else {
			enemy++
		}
	}
	fmt.Printf("opendota explorer %s: ok, %d ally / %d enemy rows above %d matches\n",
		slug, ally, enemy, cfg.OpenDota.ExplorerMinMatches)
}

// FetchOpenDota caches the explorer reconstruction per pool hero. Needs the
// stratz roster cache for hero ids; the api has been down lately, so this
// path exits with a clear message when the health check fails.
func FetchOpenDota(cfg *config.Config) error {
	rf, err := readRosterFile(cfg)
	if err != nil {
		return fmt.Errorf("stratz roster cache missing, run fetch stratz first: %w", err)
	}
	idBySlug := map[string]int{}
	for _, h := range rf.Heroes {
		idBySlug[h.Slug] = h.ID
	}
	c := newOpenDotaClient(cfg)
	if _, status, err := c.get("/api/heroes"); err != nil || status != http.StatusOK {
		return fmt.Errorf("opendota api down (status %d, %v), nothing fetched", status, err)
	}
	start := patchEpochUnix(cfg.PatchEpoch)
	if start == 0 {
		return fmt.Errorf("config patchEpoch %q is not YYYY-MM-DD", cfg.PatchEpoch)
	}
	if err := os.MkdirAll(cfg.Paths.OpenDotaRawDir, 0o755); err != nil {
		return err
	}
	done := 0
	for _, slug := range cfg.PoolSlugs() {
		id, ok := idBySlug[slug]
		if !ok {
			fmt.Printf("opendota %s: not in roster cache, skipped\n", slug)
			continue
		}
		query := buildExplorerSQL(id, cfg.Scope.GameMode, cfg.Scope.LobbyType, cfg.Scope.Explorer.RankTierMin, start)
		body, status, err := c.get("/api/explorer?sql=" + url.QueryEscape(query))
		if err != nil || status != http.StatusOK {
			fmt.Printf("opendota %s: query failed (status %d, %v), skipped\n", slug, status, err)
			continue
		}
		if err := os.WriteFile(filepath.Join(cfg.Paths.OpenDotaRawDir, slug+".json"), body, 0o644); err != nil {
			return err
		}
		if rows, err := parseExplorerResponse(body, cfg.OpenDota.ExplorerMinMatches); err == nil {
			fmt.Printf("opendota %s: %d rows above %d matches\n", slug, len(rows), cfg.OpenDota.ExplorerMinMatches)
		}
		done++
	}
	fmt.Printf("opendota fetch done: %d pool heroes cached\n", done)
	return nil
}
