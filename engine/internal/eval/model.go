// Package eval answers how accurate the picker is against real drafts: it
// rebuilds the emitted picker model at artifact precision, replays committed
// league drafts in pick order, and scores the rankings the picker would have
// produced at every pick.
package eval

import (
	"database/sql"
	"fmt"
	"path/filepath"

	"poolguide/internal/analytics"
	"poolguide/internal/config"
	"poolguide/internal/emit"
	"poolguide/internal/gates"
)

// Model is the in-memory picker model the replay scores against. Every pct
// value is the 4dp readback of exactly what picker-data-generated.js ships
// (emit.RoundFixed over the same loader), so Go replay ranks and the JS
// picker agree by construction.
type Model struct {
	RosterSize int
	Slugs      []string        // roster idx -> slug
	IdxOf      map[string]int  // slug -> roster idx
	Pop        map[int]float64 // roster idx -> pick share, rounded
	PoolIdx    []int           // pool position -> roster idx
	PoolPos    map[int]int     // roster idx -> pool position
	Roles      map[int][]string
	Mu         []map[int]float64 // pool position -> enemy roster idx -> pct
	Syn        []map[int]float64 // pool position -> ally roster idx -> pct
	Prior      []float64         // pool position -> rounded midrank percentile
	Gates      *gates.Doc
}

// BuildModel loads the derived tables through emit.LoadPicker and rerounds
// every value the artifact prints: mu/syn cells, pop, and the prior midrank
// percentile over the pool-ordered prior values, exactly as FormatPicker
// derives it.
func BuildModel(db *sql.DB, cfg *config.Config) (*Model, error) {
	doc, err := gates.Load(filepath.Join(cfg.Paths.PickerDir, "gates.json"))
	if err != nil {
		return nil, fmt.Errorf("eval: gates: %w", err)
	}
	if err := doc.Validate(); err != nil {
		return nil, fmt.Errorf("eval: %w", err)
	}
	in, err := emit.LoadPicker(db, cfg, doc)
	if err != nil {
		return nil, err
	}
	dec := cfg.Emit.Decimals
	m := &Model{
		RosterSize: len(in.Roster),
		Slugs:      make([]string, len(in.Roster)),
		IdxOf:      make(map[string]int, len(in.Roster)),
		Pop:        make(map[int]float64, len(in.Roster)),
		PoolPos:    map[int]int{},
		Roles:      map[int][]string{},
		Gates:      doc,
	}
	for i, h := range in.Roster {
		m.Slugs[i] = h.Slug
		m.IdxOf[h.Slug] = i
		share, ok := in.Pop[h.Slug]
		if !ok {
			return nil, fmt.Errorf("eval: no popularity for roster hero %s", h.Slug)
		}
		m.Pop[i] = emit.RoundFixed(share, dec)
	}
	pool := cfg.PoolSlugs()
	m.PoolIdx = make([]int, len(pool))
	m.Mu = make([]map[int]float64, len(pool))
	m.Syn = make([]map[int]float64, len(pool))
	priorVals := make([]float64, len(pool))
	for pi, slug := range pool {
		idx, ok := m.IdxOf[slug]
		if !ok {
			return nil, fmt.Errorf("eval: pool hero %s missing from roster", slug)
		}
		m.PoolIdx[pi] = idx
		m.PoolPos[idx] = pi
		m.Roles[idx] = cfg.HeroBySlug(slug).Roles
		mu, err := roundRow(in.Mu[slug], m.IdxOf, dec)
		if err != nil {
			return nil, fmt.Errorf("eval: mu %s: %w", slug, err)
		}
		syn, err := roundRow(in.Syn[slug], m.IdxOf, dec)
		if err != nil {
			return nil, fmt.Errorf("eval: syn %s: %w", slug, err)
		}
		m.Mu[pi], m.Syn[pi] = mu, syn
		v, ok := in.Prior[slug]
		if !ok {
			return nil, fmt.Errorf("eval: no prior for pool hero %s", slug)
		}
		priorVals[pi] = v
	}
	m.Prior = make([]float64, len(pool))
	for pi, p := range analytics.Midrank(priorVals) {
		m.Prior[pi] = emit.RoundFixed(p, dec)
	}
	return m, nil
}

// roundRow maps one slug-keyed cell row onto roster indices at the emitted
// precision. A partial row is an error, matching packedRow: a silent gap
// would score the model against 0s the artifact refuses to ship.
func roundRow(cells map[string]emit.PickerCell, idxOf map[string]int, dec int) (map[int]float64, error) {
	row := make(map[int]float64, len(cells))
	for slug, i := range idxOf {
		c, ok := cells[slug]
		if !ok {
			return nil, fmt.Errorf("no cell for roster hero %s", slug)
		}
		row[i] = emit.RoundFixed(c.Pct, dec)
	}
	return row, nil
}
