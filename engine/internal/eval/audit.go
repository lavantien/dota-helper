package eval

import (
	"database/sql"
	"fmt"
	"math"
	"sort"

	"poolguide/internal/config"
	"poolguide/internal/stats"
)

// PairCount is one raw pair cell, already aggregated across sources.
type PairCount struct {
	Hero, Other string
	W, N        int64
}

// AuditCell is one tested pair cell: the crawl's win record against the
// hero's remaining base inside the same family.
type AuditCell struct {
	Hero   string `json:"hero"`
	Other  string `json:"other"`
	W      int64  `json:"w"`
	N      int64  `json:"n"`
	BaseW  int64  `json:"baseW"`
	BaseN  int64  `json:"baseN"`
	P      float64 `json:"p"`
	Method string `json:"method"` // chisq | binomial
	Holm   bool   `json:"holm"`
	BH     bool   `json:"bh"`
	Lift   float64 `json:"lift"`
	Pct    *float64 `json:"pct,omitempty"`
}

// FamilyAudit is one pair family (matchup enemies, synergy allies) audited
// against its raw counts. CellsTotal records the pre-trim cell count so a
// trimmed Cells slice never undercounts silently.
type FamilyAudit struct {
	Family        string       `json:"family"`
	Tested        int          `json:"tested"`
	HolmSurvivors int          `json:"holmSurvivors"`
	BHSurvivors   int          `json:"bhSurvivors"`
	Cells         []AuditCell  `json:"cells"`
	CellsTotal    int          `json:"cellsTotal"`
	Spearman      *float64     `json:"spearman,omitempty"`
}

// auditFamily tests every qualifying cell of one family against the hero's
// remaining base: a 2x2 chi-square of (w, n) vs (W-w, N-n), falling back to
// the exact two-sided binomial when the chi-square expected counts run
// thin. Holm and BH run per family at level q; the spearman compares the
// normalized pct against the raw lift (w/n over the hero base W/N) over
// cells that carry a normalized value.
func auditFamily(family string, cells []PairCount, pcts map[string]float64, q float64, minMatches int64) FamilyAudit {
	type base struct{ w, n int64 }
	bases := map[string]*base{}
	var qualified []PairCount
	for _, c := range cells {
		if c.W < 0 || c.N < 0 || c.W > c.N || c.N < minMatches {
			continue
		}
		qualified = append(qualified, c)
		b := bases[c.Hero]
		if b == nil {
			b = &base{}
			bases[c.Hero] = b
		}
		b.w += c.W
		b.n += c.N
	}
	sort.Slice(qualified, func(a, b int) bool {
		if qualified[a].Hero != qualified[b].Hero {
			return qualified[a].Hero < qualified[b].Hero
		}
		return qualified[a].Other < qualified[b].Other
	})

	fam := FamilyAudit{Family: family, Cells: []AuditCell{}}
	var ps []float64
	var pctVals, liftVals []float64
	for _, c := range qualified {
		b := bases[c.Hero]
		cn, cw := b.n-c.N, b.w-c.W
		if cn <= 0 {
			continue
		}
		cell := AuditCell{Hero: c.Hero, Other: c.Other, W: c.W, N: c.N, BaseW: cw, BaseN: cn}
		_, p, ok := stats.ChiSquare2x2(float64(c.W), float64(c.N-c.W), float64(cw), float64(cn-cw))
		if ok {
			cell.Method = "chisq"
		} else {
			cell.Method = "binomial"
			p = stats.BinomialTwoSided(int(c.W), int(c.N), float64(cw)/float64(cn))
		}
		cell.P = p
		// lift needs a positive hero base rate: an all-loss base leaves it
		// undefined and a NaN would poison both the field and the spearman
		if b.w > 0 {
			cell.Lift = (float64(c.W) / float64(c.N)) / (float64(b.w) / float64(b.n))
		}
		if pct, ok := pcts[c.Hero+"\x00"+c.Other]; ok && b.w > 0 {
			v := pct
			cell.Pct = &v
			pctVals = append(pctVals, v)
			liftVals = append(liftVals, cell.Lift)
		}
		fam.Cells = append(fam.Cells, cell)
		ps = append(ps, p)
	}
	fam.Tested = len(ps)
	if len(ps) > 0 {
		holm := stats.Holm(ps, q)
		bh := stats.BH(ps, q)
		for i := range fam.Cells {
			fam.Cells[i].Holm = holm[i]
			fam.Cells[i].BH = bh[i]
			if holm[i] {
				fam.HolmSurvivors++
			}
			if bh[i] {
				fam.BHSurvivors++
			}
		}
	}
	if len(pctVals) > 1 {
		// constant input on either side has no rank variance: report no
		// correlation instead of a NaN the report cannot marshal
		if r := stats.Spearman(pctVals, liftVals); !math.IsNaN(r) {
			fam.Spearman = &r
		}
	}
	return fam
}

// Audit loads the raw pair tables and audits both families: matchup cells
// against the hero's remaining vs-pool, synergy cells against the
// with-pool. Screen thresholds come from the config hub.
func Audit(db *sql.DB, cfg *config.Config) ([]FamilyAudit, error) {
	q := cfg.Eval.Screen.Q
	minN := int64(cfg.Eval.Screen.MinMatches)

	// every matchup source serving win_count is hero-perspective (the
	// stratz_vs adjudication lives at mine.canonicalPP), so counts pass
	// through unchanged
	matchups := map[string]*PairCount{}
	rows, err := db.Query(`SELECT pool_slug, enemy_slug, win_count, matches
		FROM matchup_raw WHERE win_count IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("matchup_raw: %w", err)
	}
	for rows.Next() {
		var hero, other string
		var w, n int64
		if err := rows.Scan(&hero, &other, &w, &n); err != nil {
			rows.Close()
			return nil, err
		}
		addCount(matchups, hero, other, w, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	synergies := map[string]*PairCount{}
	rows, err = db.Query(`SELECT hero_a, hero_b, win_count, match_count
		FROM synergy_raw WHERE win_count IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("synergy_raw: %w", err)
	}
	for rows.Next() {
		var hero, other string
		var w, n int64
		if err := rows.Scan(&hero, &other, &w, &n); err != nil {
			rows.Close()
			return nil, err
		}
		addCount(synergies, hero, other, w, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	pctEnemy, pctAlly, err := loadNormPcts(db)
	if err != nil {
		return nil, err
	}
	return []FamilyAudit{
		auditFamily("matchup", flatten(matchups), pctEnemy, q, minN),
		auditFamily("synergy", flatten(synergies), pctAlly, q, minN),
	}, nil
}

func addCount(m map[string]*PairCount, hero, other string, w, n int64) {
	key := hero + "\x00" + other
	c := m[key]
	if c == nil {
		c = &PairCount{Hero: hero, Other: other}
		m[key] = c
	}
	c.W += w
	c.N += n
}

func flatten(m map[string]*PairCount) []PairCount {
	out := make([]PairCount, 0, len(m))
	for _, c := range m {
		out = append(out, *c)
	}
	return out
}

func loadNormPcts(db *sql.DB) (enemy, ally map[string]float64, err error) {
	enemy, ally = map[string]float64{}, map[string]float64{}
	rows, err := db.Query(`SELECT hero, other, kind, pct FROM norm_cell`)
	if err != nil {
		return nil, nil, fmt.Errorf("norm_cell: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var hero, other, kind string
		var pct float64
		if err := rows.Scan(&hero, &other, &kind, &pct); err != nil {
			return nil, nil, err
		}
		switch kind {
		case "enemy":
			enemy[hero+"\x00"+other] = pct
		case "ally":
			ally[hero+"\x00"+other] = pct
		}
	}
	return enemy, ally, rows.Err()
}
