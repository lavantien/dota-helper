package ingest

import (
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"poolguide/internal/config"
)

// Positions crawl: five winDay slices, one per farm position, under the same
// bracket and mode scope as the popularity aggregate. Probe-pinned
// 2026-09-27 (make probe-scope): positionIds slices the aggregate but rows
// are (day, heroId, winCount, matchCount) buckets and never carry the
// position, so one request per position is the only shape; gameModeIds takes
// the enum token and bracketIds the two spelled-out rank brackets.

const positionsField = "winDay"

type positionRows struct {
	Position int      `json:"position"`
	Rows     []popRow `json:"rows"`
}

type positionsFile struct {
	FetchedAt string         `json:"fetchedAt"`
	Bracket   string         `json:"bracket"`
	GameMode  int            `json:"gameMode"`
	Take      int            `json:"take"`
	Field     string         `json:"field"`
	Positions []positionRows `json:"positions"`
}

func positionQuery(k, take int, brackets, mode string) string {
	return fmt.Sprintf(
		"query { heroStats { %s(take: %d, bracketIds: [%s], gameModeIds: [%s], positionIds: [POSITION_%d]) { heroId matchCount winCount } } }",
		positionsField, take, brackets, mode, k)
}

// fetchPositionsFrom queries the five position slices and folds each day
// bucket set into per-hero totals, hero id ascending.
func fetchPositionsFrom(c gqlFetcher, cfg *config.Config) (positionsFile, error) {
	mode, ok := gameModeEnumToken(cfg.Scope.GameMode)
	if !ok {
		return positionsFile{}, fmt.Errorf("positions: gameMode %d has no known enum token", cfg.Scope.GameMode)
	}
	brackets, ok := winBracketIds(cfg.Scope.Bracket)
	if !ok {
		return positionsFile{}, fmt.Errorf("positions: bracket %q has no win* bracketIds mapping", cfg.Scope.Bracket)
	}
	pf := positionsFile{
		FetchedAt: time.Now().UTC().Format(time.RFC3339),
		Bracket:   cfg.Scope.Bracket,
		GameMode:  cfg.Scope.GameMode,
		Take:      cfg.Stratz.Take,
		Field:     positionsField,
	}
	for k := 1; k <= 5; k++ {
		body, err := c.query(positionQuery(k, cfg.Stratz.Take, brackets, mode))
		if err != nil {
			return positionsFile{}, fmt.Errorf("positions %d: %w", k, err)
		}
		rows, err := parsePopRows(body)
		if err != nil {
			return positionsFile{}, fmt.Errorf("positions %d: %w", k, err)
		}
		byHero := popRowsByHero(rows)
		folded := make([]popRow, 0, len(byHero))
		for _, r := range byHero {
			folded = append(folded, r)
		}
		sort.Slice(folded, func(i, j int) bool { return folded[i].HeroID < folded[j].HeroID })
		pf.Positions = append(pf.Positions, positionRows{Position: k, Rows: folded})
	}
	return pf, nil
}

// FetchPositions runs the positions crawl and writes the raw cache.
func FetchPositions(cfg *config.Config) error {
	token, err := loadToken(cfg)
	if err != nil {
		return err
	}
	client := newStratzClient(cfg, token)
	pf, err := fetchPositionsFrom(client, cfg)
	if err != nil {
		return err
	}
	if err := writeJson(filepath.Join(cfg.Paths.PositionsRawDir, "_positions.json"), &pf); err != nil {
		return err
	}
	fmt.Printf("stratz positions: %d positions x %d..%d hero rows via %s scoped to %s + gameMode %d\n",
		len(pf.Positions), len(pf.Positions[0].Rows), len(pf.Positions[4].Rows), pf.Field, pf.Bracket, pf.GameMode)
	return nil
}
