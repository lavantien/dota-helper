package eval

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"time"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
	"poolguide/internal/emit"
	"poolguide/internal/mine"
	"poolguide/internal/stats"
)

// CompletionCandidate is one grid point scored by masked-cell CV.
type CompletionCandidate struct {
	Rank     int     `json:"rank"`
	Lambda   float64 `json:"lambda"`
	Spearman float64 `json:"spearman"`
}

// CompletionProposal is the completion fit artifact written beside
// paths.fitOut.
type CompletionProposal struct {
	Kinds      []string            `json:"kinds"`
	Masked     int                 `json:"masked"`
	MaskFrac   float64             `json:"maskFrac"`
	Current    CompletionCandidate   `json:"current"`
	Winner     CompletionCandidate   `json:"winner"`
	Grid       []CompletionCandidate `json:"grid"`
	DiffCiLow  float64             `json:"diffCiLow"`
	DiffCiHi   float64             `json:"diffCiHi"`
	MeasuredAt string              `json:"measuredAt"`
}

// heldCell is one masked cell with its actual shrunk value and the
// predictions of the winning and the current candidates.
type heldCell struct {
	kind, row, col  int
	actual          float64
	winner, current float64
}

// loadShrunkMatrices loads the per-kind observed matrices from pair_shrunk
// with the cell membership NormalizeMatrix completes: rows are the pool
// (slug ascending), columns the roster in hero_id order, a cell is observed
// when it exists with n >= thresholds.thinCellMatches.
func loadShrunkMatrices(db *sql.DB, cfg *config.Config) ([]string, []analytics.Matrix, error) {
	kinds := []string{mine.KindEnemy, mine.KindAlly}
	pool := cfg.PoolSlugs()
	poolAt := make(map[string]int, len(pool))
	for i, s := range pool {
		poolAt[s] = i
	}
	var roster []string
	rows, err := db.Query(`SELECT slug FROM hero_roster ORDER BY hero_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("hero_roster: %w", err)
	}
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			rows.Close()
			return nil, nil, err
		}
		roster = append(roster, slug)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	mats := make([]analytics.Matrix, len(kinds))
	for i := range mats {
		mats[i].Values = make([][]float64, len(pool))
		mats[i].Observed = make([][]bool, len(pool))
		for r := range mats[i].Values {
			mats[i].Values[r] = make([]float64, len(roster))
			mats[i].Observed[r] = make([]bool, len(roster))
		}
	}
	colAt := make(map[string]int, len(roster))
	for j, s := range roster {
		colAt[s] = j
	}
	thin := int64(cfg.Thresholds.ThinCellMatches)
	rows, err = db.Query(`SELECT hero, other, kind, value, n FROM pair_shrunk`)
	if err != nil {
		return nil, nil, fmt.Errorf("pair_shrunk: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hero, other, kind string
		var v float64
		var n int64
		if err := rows.Scan(&hero, &other, &kind, &v, &n); err != nil {
			return nil, nil, err
		}
		r, okR := poolAt[hero]
		c, okC := colAt[other]
		ki := -1
		for i, k := range kinds {
			if k == kind {
				ki = i
			}
		}
		if !okR || !okC || ki < 0 || n < thin {
			continue
		}
		mats[ki].Values[r][c] = v
		mats[ki].Observed[r][c] = true
	}
	return kinds, mats, rows.Err()
}

// maskMatrix splits one observed matrix into a training matrix and the
// held-out cells: a seeded shuffle of the row-major observed order keeps the
// split deterministic, and the held-out count is the truncated maskFrac
// fraction of the observed cells.
func maskMatrix(m analytics.Matrix, maskFrac float64, rng *rand.Rand) (analytics.Matrix, []heldCell) {
	train := analytics.Matrix{
		Values:   make([][]float64, len(m.Values)),
		Observed: make([][]bool, len(m.Values)),
	}
	var order []int
	width := 0
	if len(m.Values) > 0 {
		width = len(m.Values[0])
	}
	for i := range m.Values {
		train.Values[i] = append([]float64(nil), m.Values[i]...)
		train.Observed[i] = append([]bool(nil), m.Observed[i]...)
		for j := range m.Observed[i] {
			if m.Observed[i][j] {
				order = append(order, i*width+j)
			}
		}
	}
	rng.Shuffle(len(order), func(a, b int) { order[a], order[b] = order[b], order[a] })
	hold := int(maskFrac * float64(len(order)))
	var held []heldCell
	for _, pos := range order[:hold] {
		i, j := pos/width, pos%width
		train.Observed[i][j] = false
		held = append(held, heldCell{row: i, col: j, actual: m.Values[i][j]})
	}
	return train, held
}

// completionCV fits every (rank, lambda) grid point by ALS on the masked
// training matrices, reusing analytics.Complete, and scores each candidate
// by the mean across kinds of stats.Spearman between the predicted and
// held-out shrunk values. Ties keep the smaller rank, then the smaller
// lambda. The current config candidate is scored on the same mask.
func completionCV(kinds []string, mats []analytics.Matrix, fc config.EvalCompletionFitCfg, base config.CompletionCfg, maskSeed int64) (winner, current CompletionCandidate, grid []CompletionCandidate, held [][]heldCell, err error) {
	rng := rand.New(rand.NewSource(maskSeed))
	train := make([]analytics.Matrix, len(mats))
	held = make([][]heldCell, len(mats))
	for i, m := range mats {
		train[i], held[i] = maskMatrix(m, fc.MaskFrac, rng)
	}
	score := func(rank int, lambda float64) (float64, bool) {
		sum, n := 0.0, 0
		for i := range train {
			cc := base
			cc.Rank, cc.Lambda = rank, lambda
			done := analytics.Complete(train[i], cc)
			var pred, actual []float64
			for _, c := range held[i] {
				pred = append(pred, done.Values[c.row][c.col])
				actual = append(actual, c.actual)
			}
			if len(pred) < 2 {
				continue
			}
			if r := stats.Spearman(pred, actual); !math.IsNaN(r) {
				sum += r
				n++
			}
		}
		if n == 0 {
			return 0, false
		}
		return sum / float64(n), true
	}
	best := false
	for _, rank := range fc.Ranks {
		for _, lambda := range fc.Lambdas {
			s, ok := score(rank, lambda)
			if !ok {
				continue
			}
			grid = append(grid, CompletionCandidate{Rank: rank, Lambda: lambda, Spearman: s})
			if !best || s > winner.Spearman {
				winner = CompletionCandidate{Rank: rank, Lambda: lambda, Spearman: s}
				best = true
			}
		}
	}
	if !best {
		return winner, current, grid, held, fmt.Errorf("completion cv: no grid candidate produced a spearman")
	}
	cs, ok := score(base.Rank, base.Lambda)
	if !ok {
		return winner, current, grid, held, fmt.Errorf("completion cv: the current candidate produced no spearman")
	}
	current = CompletionCandidate{Rank: base.Rank, Lambda: base.Lambda, Spearman: cs}
	for i := range train {
		w := analytics.Complete(train[i], override(base, winner.Rank, winner.Lambda))
		c := analytics.Complete(train[i], override(base, current.Rank, current.Lambda))
		for j := range held[i] {
			held[i][j].kind = i
			held[i][j].winner = w.Values[held[i][j].row][held[i][j].col]
			held[i][j].current = c.Values[held[i][j].row][held[i][j].col]
		}
	}
	return winner, current, grid, held, nil
}

func override(base config.CompletionCfg, rank int, lambda float64) config.CompletionCfg {
	base.Rank, base.Lambda = rank, lambda
	return base
}

// FitCompletion runs the masked-cell CV over the shrunk tables and writes
// the proposal beside paths.fitOut.
func FitCompletion(d Deps) (string, error) {
	kinds, mats, err := loadShrunkMatrices(d.DB, d.Cfg)
	if err != nil {
		return "", err
	}
	winner, current, grid, held, err := completionCV(kinds, mats, d.Cfg.Eval.CompletionFit, d.Cfg.Completion, d.Cfg.Eval.Bootstrap.Seed)
	if err != nil {
		return "", err
	}
	var flat []heldCell
	for _, hs := range held {
		flat = append(flat, hs...)
	}
	// bootstrap the winner-minus-current spearman difference over held-out
	// cell resamples; a resample with constant ranks carries no information
	// and contributes 0
	b := d.Cfg.Eval.Bootstrap
	lo, hi := stats.PercentileCI(func(idx []int) float64 {
		var pw, pc, act []float64
		for _, i := range idx {
			pw = append(pw, flat[i].winner)
			pc = append(pc, flat[i].current)
			act = append(act, flat[i].actual)
		}
		dw, dc := stats.Spearman(pw, act), stats.Spearman(pc, act)
		if math.IsNaN(dw) || math.IsNaN(dc) {
			return 0
		}
		return dw - dc
	}, len(flat), b.Resamples, uint64(b.Seed), 2.5, 97.5)

	prop := CompletionProposal{
		Kinds: kinds, Masked: len(flat), MaskFrac: d.Cfg.Eval.CompletionFit.MaskFrac,
		Current: current, Winner: winner, Grid: grid, DiffCiLow: lo, DiffCiHi: hi,
		MeasuredAt: time.Now().UTC().Format(time.RFC3339),
	}
	out := filepath.Join(d.RepoRoot, fitProposalPath(d.Cfg.Paths.FitOut, "completion"))
	jb, err := json.MarshalIndent(prop, "", "  ")
	if err != nil {
		return "", err
	}
	if err := emit.WriteFile(out, append(jb, '\n')); err != nil {
		return "", fmt.Errorf("fit completion: write proposal: %w", err)
	}
	verdict := "guard FAILS (keep current values)"
	if lo > 0 && (winner.Rank != current.Rank || winner.Lambda != current.Lambda) {
		verdict = "guard passes (eval promote completion may apply)"
	}
	return fmt.Sprintf("completion fit: %d masked cells over %s\nwinner rank %d lambda %g spearman %.4f, current rank %d lambda %g spearman %.4f\ndifference CI [%.4f, %.4f]: %s\n",
		prop.Masked, kinds, winner.Rank, winner.Lambda, winner.Spearman,
		current.Rank, current.Lambda, current.Spearman, lo, hi, verdict), nil
}
