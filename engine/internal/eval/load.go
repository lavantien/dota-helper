package eval

import (
	"database/sql"
	"fmt"
)

// DraftEvent is one pick/ban row of a committed draft, hero ids already
// resolved to roster slugs by LoadMatches.
type DraftEvent struct {
	Seq       int
	IsRadiant bool
	IsPick    bool
	HeroID    int
	Slug      string
}

// Match is one ingested league draft with its population fields.
type Match struct {
	ID          int64
	StartTime   int64
	RadiantWin  bool
	DurationSec int
	GameMode    string
	LobbyType   string
	Bracket     string
	Source      string
	AverageRank int // 0 reads as null
	Events      []DraftEvent
}

// LoadStats accounts for every match_raw row: what loaded and what dropped,
// so the population profile never undercounts silently.
type LoadStats struct {
	Total            int
	Loaded           int
	DroppedNoDraft   int // no draft_timing rows at all
	DroppedUnknown   int // a hero id the roster table does not resolve
	DroppedDuplicate int // the same hero twice across picks and bans
}

// LoadMatches joins match_raw with draft_timing in seq order, resolves hero
// ids through hero_roster, and returns matches ordered by (start_time,
// match_id) plus the drop accounting. Matches with unknown hero ids or any
// repeated hero across picks and bans are dropped: both are crawl
// corruption, and a duplicated pick would poison the taken set every later
// pick scores against.
func LoadMatches(db *sql.DB) ([]Match, LoadStats, error) {
	var stats LoadStats
	slugByID := map[int]string{}
	rows, err := db.Query(`SELECT hero_id, slug FROM hero_roster`)
	if err != nil {
		return nil, stats, fmt.Errorf("hero_roster: %w", err)
	}
	for rows.Next() {
		var id int
		var slug string
		if err := rows.Scan(&id, &slug); err != nil {
			rows.Close()
			return nil, stats, err
		}
		slugByID[id] = slug
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, stats, err
	}
	rows.Close()

	query := `SELECT m.match_id, m.start_time, m.radiant_win, m.duration_seconds,
		COALESCE(m.game_mode, ''), COALESCE(m.lobby_type, ''), m.bracket, m.source,
		COALESCE(m.average_rank, 0), d.seq, d.is_radiant, d.is_pick, d.hero_id
		FROM match_raw m LEFT JOIN draft_timing d ON d.match_id = m.match_id
		ORDER BY m.start_time, m.match_id, d.seq`
	rows, err = db.Query(query)
	if err != nil {
		return nil, stats, fmt.Errorf("match_raw join: %w", err)
	}
	defer rows.Close()
	var ordered []Match
	cur := Match{ID: -1}
	flush := func() {
		if cur.ID >= 0 {
			ordered = append(ordered, cur)
		}
		cur = Match{ID: -1}
	}
	for rows.Next() {
		var m Match
		var ev DraftEvent
		var seq, heroID sql.NullInt64
		var isRadiant, isPick sql.NullBool
		if err := rows.Scan(&m.ID, &m.StartTime, &m.RadiantWin, &m.DurationSec,
			&m.GameMode, &m.LobbyType, &m.Bracket, &m.Source, &m.AverageRank,
			&seq, &isRadiant, &isPick, &heroID); err != nil {
			return nil, stats, err
		}
		if m.ID != cur.ID {
			flush()
			cur = m
		}
		if !seq.Valid {
			continue // the left-join row of a match with no draft
		}
		ev.Seq, ev.IsRadiant, ev.IsPick, ev.HeroID = int(seq.Int64), isRadiant.Bool, isPick.Bool, int(heroID.Int64)
		ev.Slug = slugByID[ev.HeroID]
		cur.Events = append(cur.Events, ev)
	}
	flush()
	if err := rows.Err(); err != nil {
		return nil, stats, err
	}

	var out []Match
	for _, m := range ordered {
		stats.Total++
		if len(m.Events) == 0 {
			stats.DroppedNoDraft++
			continue
		}
		seen := map[string]bool{}
		unknown, duplicate := false, false
		for _, ev := range m.Events {
			if ev.Slug == "" {
				unknown = true
			} else if seen[ev.Slug] {
				duplicate = true
			}
			seen[ev.Slug] = true
		}
		switch {
		case unknown:
			stats.DroppedUnknown++
		case duplicate:
			stats.DroppedDuplicate++
		default:
			out = append(out, m)
			stats.Loaded++
		}
	}
	return out, stats, nil
}
