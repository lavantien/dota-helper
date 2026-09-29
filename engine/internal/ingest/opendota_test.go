package ingest

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExplorerSQLGolden(t *testing.T) {
	sql := buildExplorerSQL(8, 22, 7, 71, 1757894400)
	want := `SELECT CASE WHEN (pa.player_slot < 128) = (pb.player_slot < 128) THEN 'ally' ELSE 'enemy' END AS kind, ` +
		`pb.hero_id AS other_id, count(*) AS matches, ` +
		`sum(CASE WHEN (pa.player_slot < 128) = m.radiant_win THEN 1 ELSE 0 END) AS pool_wins ` +
		`FROM player_matches pa JOIN player_matches pb ON pb.match_id = pa.match_id AND pb.hero_id <> 8 ` +
		`JOIN matches m ON m.match_id = pa.match_id ` +
		`WHERE pa.hero_id = 8 AND pa.start_time >= 1757894400 ` +
		`AND pa.lobby_type = 7 AND pa.game_mode = 22 AND pa.rank_tier >= 71 ` +
		`GROUP BY 1, 2`
	if sql != want {
		t.Fatalf("explorer sql mismatch\n got: %s\nwant: %s", sql, want)
	}
}

func TestPatchEpochUnix(t *testing.T) {
	got := patchEpochUnix("2026-09-15")
	want := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC).Unix()
	if got != want {
		t.Fatalf("patchEpochUnix = %d, want %d", got, want)
	}
}

func TestParseExplorerResponse(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "opendota_explorer.json"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := parseExplorerResponse(b, 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows after min-matches filter, want 2", len(rows))
	}
	if rows[0].Kind != "ally" || rows[0].OtherID != 14 || rows[0].Matches != 120 || rows[0].PoolWins != 70 {
		t.Errorf("rows[0] = %+v, want ally hero 14 with 120 matches and 70 wins", rows[0])
	}
	if rows[1].Kind != "enemy" || rows[1].OtherID != 8 || rows[1].Matches != 300 || rows[1].PoolWins != 160 {
		t.Errorf("rows[1] = %+v, want enemy hero 8 with 300 matches and 160 wins", rows[1])
	}
}
