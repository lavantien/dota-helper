package eval

import (
	"database/sql"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"poolguide/internal/analytics"
	"poolguide/internal/mine"
)

func TestMomentAlphaHandComputed(t *testing.T) {
	ds := []float64{60, 40, 60, 40}
	ns := []float64{100, 100, 100, 100}
	alpha, tau2, kept := momentAlpha(ds, ns, 2500)
	if kept {
		t.Fatal("moment fit fell back on a signal-bearing family")
	}
	if math.Abs(tau2-75) > 1e-9 || math.Abs(alpha-2500.0/75) > 1e-9 {
		t.Fatalf("moment alpha %v tau2 %v, want %v and 75", alpha, tau2, 2500.0/75)
	}
	alpha, tau2, kept = momentAlpha([]float64{50, 50, 50, 50}, ns, 2500)
	if !kept || alpha != 0 || tau2 != 0 {
		t.Fatalf("degenerate family gave alpha %v tau2 %v kept %v", alpha, tau2, kept)
	}
}

func TestBinomialDevianceHandComputed(t *testing.T) {
	cases := []struct {
		w, n int64
		p    float64
		want float64
	}{
		{50, 100, 0.5, 0},
		{60, 100, 0.5, 2 * (60*math.Log(1.2) + 40*math.Log(0.8))},
		{0, 10, 0.5, 2 * 10 * math.Log(2)},
		{10, 10, 0.9, 2 * 10 * math.Log(10.0/9.0)},
	}
	for _, c := range cases {
		if got := binomialDeviance(c.w, c.n, c.p); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("deviance(%d, %d, %v) = %v, want %v", c.w, c.n, c.p, got, c.want)
		}
	}
}

func TestCVAlphaRecoversPlantedShrinkage(t *testing.T) {
	_, cfg := handModel(t)
	grid := cfg.Eval.AlphaFit.Grid
	const planted = 40.0
	effects := []float64{8, -8, 5, -5, 12, -12, 3, -3, 10, -10, 6, -6}
	var obs []cvObs
	for i := 0; len(obs) < 40; i++ {
		e := effects[i%len(effects)]
		p := 0.5 + analytics.ShrinkDelta(e, 100, planted)/100
		w := int64(math.Round(p * 200))
		obs = append(obs, cvObs{d: e, nAgg: 100, w: w, nHold: 200})
	}
	fit := cvAlpha(grid, 30, obs, cfg.Shrink.MatchupAlpha, cfg.Score.MidPct)
	if !fit.Adequate || fit.Pairs != len(obs) {
		t.Fatalf("cv fit %+v, want adequate over %d pairs", fit, len(obs))
	}
	if fit.Alpha != planted {
		t.Fatalf("cv chose alpha %v, want the planted %v", fit.Alpha, planted)
	}
	if fit := cvAlpha(grid, 30, obs[:1], cfg.Shrink.MatchupAlpha, cfg.Score.MidPct); fit.Adequate {
		t.Fatal("a single qualifying pair must read as inadequate")
	}
}

func TestOverallDeltasMirrorOverallWRSums(t *testing.T) {
	cfg := testConfig(t)
	pool := cfg.PoolSlugs()
	hero := pool[0]
	var rows []mine.MatchupRaw
	for i := 0; i < 5; i++ {
		rows = append(rows, mine.MatchupRaw{
			PoolSlug: hero, EnemySlug: pool[(i+1)%len(pool)],
			WinCount: sql.NullInt64{Int64: 40, Valid: true},
			Synergy:  sql.NullFloat64{Float64: -3, Valid: true},
			Matches:  100, Source: mine.SrcStratzVS, Confidence: mine.ConfHigh,
		})
	}
	rows = append(rows, mine.MatchupRaw{
		PoolSlug: hero, EnemySlug: pool[1], Dis: sql.NullFloat64{Float64: 2, Valid: true},
		Matches: 500, Source: mine.SrcOpenDotaEp, Confidence: mine.ConfHigh,
	})
	deltas := overallDeltas(rows, cfg)
	if len(deltas) != 1 || deltas[0].Hero != hero {
		t.Fatalf("overall deltas %+v, want exactly the vs-covered %s", deltas, hero)
	}
	if deltas[0].N != 100 || math.Abs(deltas[0].Delta+10) > 1e-9 {
		t.Fatalf("overall delta %+v, want n 100 delta -10pp", deltas[0])
	}
	wr := mine.OverallWR(rows, []string{hero}, cfg)[hero]
	if want := analytics.Shrink(40, 100, cfg.Shrink.OverallWrAlpha, 0.5); math.Abs(wr-want) > 1e-12 {
		t.Fatalf("mined wr %v does not shrink the same sums the family pooled (want %v)", wr, want)
	}
}

func TestFitAlphasWritesProposal(t *testing.T) {
	t.Chdir(repoRoot)
	cfg := testConfig(t)
	cfg.Eval.Bootstrap.Resamples = 50
	db := seedModelDB(t)
	seedMatches(t, db)
	pool := cfg.PoolSlugs()
	roster := seedRoster(t, cfg)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("exec: %v", err)
		}
	}
	for i, hero := range pool {
		for j, other := range roster {
			if hero == other {
				continue
			}
			syn := 6.0 + 2*float64(i%3)
			if (i+j)%2 == 1 {
				syn = -syn
			}
			wins := int64(500 + 25*(i%3) + 10*(j%2))
			exec(`INSERT INTO matchup_raw (pool_slug, enemy_slug, win_count, synergy, matches, source, scrape_date, confidence)
				VALUES (?, ?, ?, ?, ?, 'stratz_vs', '2026-09-20', 'high')`,
				hero, other, wins, syn, 1000)
		}
	}
	for i, hero := range pool {
		for j, other := range roster {
			if hero == other {
				continue
			}
			syn := 4.0
			if (i+j)%2 == 1 {
				syn = -4
			}
			exec(`INSERT INTO synergy_raw (hero_a, hero_b, match_count, synergy, source)
				VALUES (?, ?, ?, ?, 'stratz')`, hero, other, 800, syn)
		}
	}
	idOf := map[string]int{}
	for _, slug := range roster {
		var id int
		if err := db.QueryRow(`SELECT hero_id FROM hero_roster WHERE slug = ?`, slug).Scan(&id); err != nil {
			t.Fatalf("hero id for %s: %v", slug, err)
		}
		idOf[slug] = id
	}
	for mi := 0; mi < 100; mi++ {
		matchID := int64(3000 + mi)
		start := int64(1700000000 + 86400*(10+mi))
		exec(`INSERT INTO match_raw
			(match_id, start_time, radiant_win, duration_seconds, lobby_type, game_mode, bracket, average_rank, source, fetched_at)
			VALUES (?, ?, ?, 2400, 'PRACTICE', 'CAPTAINS_MODE', 'TEST', 30, 'stratz_backfill', 'test')`,
			matchID, start, mi%2 == 0)
		for seq := 0; seq < 10; seq++ {
			slug := pool[0]
			if seq > 0 {
				slug = pool[1+(mi+seq)%(len(pool)-1)]
			}
			exec(`INSERT INTO draft_timing (match_id, seq, is_radiant, is_pick, hero_id)
				VALUES (?, ?, ?, ?, ?)`, matchID, seq, seq%2 == 0, true, idOf[slug])
		}
	}

	run := func(root string) AlphaProposal {
		t.Helper()
		if _, err := FitAlphas(Deps{DB: db, Cfg: cfg, RepoRoot: root}); err != nil {
			t.Fatalf("fit alphas: %v", err)
		}
		path := filepath.Join(root, fitProposalPath(cfg.Paths.FitOut, "alphas"))
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read proposal: %v", err)
		}
		var p AlphaProposal
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatalf("parse proposal: %v", err)
		}
		return p
	}
	a := run(t.TempDir())
	if len(a.Families) != 3 {
		t.Fatalf("%d families, want matchup, synergy, overall", len(a.Families))
	}
	byName := map[string]AlphaFamilyResult{}
	for _, f := range a.Families {
		byName[f.Family] = f
	}
	cur := map[string]float64{
		"matchup": cfg.Shrink.MatchupAlpha, "synergy": cfg.Shrink.SynergyAlpha, "overall": cfg.Shrink.OverallWrAlpha,
	}
	for name, want := range cur {
		f := byName[name]
		if f.Current != want {
			t.Errorf("%s current %v, want the config value %v", name, f.Current, want)
		}
		if f.Chosen <= 0 {
			t.Errorf("%s chose %v, want a positive alpha", name, f.Chosen)
		}
		if f.Source == "" || f.Reason == "" {
			t.Errorf("%s carries no source or reason", name)
		}
	}
	if byName["matchup"].CV.Adequate {
		t.Error("thin holdout pairs read as adequate CV coverage")
	}
	if byName["overall"].CV.Pairs < 1 {
		t.Error("overall family joined no holdout records, the hero-level key is broken")
	}
	b := run(t.TempDir())
	for i := range a.Families {
		if a.Families[i].Chosen != b.Families[i].Chosen {
			t.Fatalf("family %s drifted between runs: %v vs %v", a.Families[i].Family, a.Families[i].Chosen, b.Families[i].Chosen)
		}
	}
}
