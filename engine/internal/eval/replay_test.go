package eval

import (
	"math"
	"strconv"
	"testing"

	"poolguide/internal/config"
	"poolguide/internal/gates"
)

// handModel is the 8-hero roster / 3-hero pool fixture with constant 0.5 mu
// and syn everywhere, so every score reduces to the ramp terms:
//
//	score = 0.1*k + 0.07*a + 0.5*prior + 0.2*(4-a)/4 + gateDelta
//
// with weights 1.0/0.7/0.5/0.4/0.5/0.3 and gate deltas +-0.25 pinned here
// (the hub's weights are fitted and move; the hand arithmetic must not).
// Priors: h1 0.7, h2 0.9, h3 0.5.
func handModel(t *testing.T, extraRules ...gates.Rule) (*Model, *config.Config) {
	t.Helper()
	cfg := testConfig(t)
	cfg.Weights = config.Weights{
		KnownMu: 1.0, Prior: 0.5, GenericFit: 0.4, Exposure: 0.5, Flexibility: 0.3,
		SynByRole: map[string]float64{"1": 0.7, "2": 0.7, "3": 0.7, "4": 0.7, "5": 0.7},
	}
	cfg.GateDeltas = map[string]float64{"bonus": 0.25, "penalty": -0.25}
	// same pin as the weights: the hub's shared role pools are policy, the
	// hand arithmetic must not move when policy does
	cfg.SharedRolePools = nil
	slugs := make([]string, 8)
	for i := range slugs {
		slugs[i] = "h" + strconv.Itoa(i)
	}
	m := &Model{
		RosterSize: 8,
		Slugs:      slugs,
		IdxOf:      map[string]int{},
		Pop:        map[int]float64{},
		PoolPos:    map[int]int{},
		Roles:      map[int][]string{},
		Gates: &gates.Doc{
			Rules: append([]gates.Rule{
				{Id: "t-h3-veto", Target: "h3", When: []gates.Condition{
					{Kind: gates.KindEnemyVisibleAny, Heroes: []string{"h7"}},
				}, Action: gates.ActionHardGate},
				{Id: "t-h1-pen", Target: "h1", When: []gates.Condition{
					{Kind: gates.KindEnemyVisibleAny, Heroes: []string{"h6"}},
				}, Action: gates.ActionPenalty},
				{Id: "t-h2-bon", Target: "h2", When: []gates.Condition{
					{Kind: gates.KindRoleCandidatesGated, Count: 0},
				}, Action: gates.ActionBonus},
			}, extraRules...),
		},
	}
	for i, s := range slugs {
		m.IdxOf[s] = i
		m.Pop[i] = 0.125
	}
	roles := map[string][]string{"h1": {"1"}, "h2": {"1"}, "h3": {"2"}}
	priors := map[string]float64{"h1": 0.7, "h2": 0.9, "h3": 0.5}
	for _, s := range []string{"h1", "h2", "h3"} {
		idx := m.IdxOf[s]
		m.PoolIdx = append(m.PoolIdx, idx)
		m.PoolPos[idx] = len(m.PoolIdx) - 1
		m.Roles[idx] = roles[s]
		mu := map[int]float64{}
		syn := map[int]float64{}
		for i := range slugs {
			mu[i] = 0.5
			syn[i] = 0.5
		}
		m.Mu = append(m.Mu, mu)
		m.Syn = append(m.Syn, syn)
		m.Prior = append(m.Prior, priors[s])
	}
	return m, cfg
}

// handDraft is one synthetic draft of 2 bans + 6 picks. Pool picks: h2
// (seq2, k=0 a=0), h1 (seq6, k=2 a=1, after 2 visible enemies), h3 (seq7,
// k=3 a=0).
func handDraft() Match {
	ev := func(seq int, rad, pick bool, slug string) DraftEvent {
		return DraftEvent{Seq: seq, IsRadiant: rad, IsPick: pick, Slug: slug, HeroID: 100 + seq}
	}
	return Match{
		ID: 1, StartTime: 1000, RadiantWin: true,
		GameMode: "CAPTAINS_MODE", LobbyType: "PRACTICE", Bracket: "TEST", AverageRank: 30,
		Events: []DraftEvent{
			ev(0, true, false, "h4"),
			ev(1, false, false, "h5"),
			ev(2, true, true, "h2"),
			ev(3, false, true, "h6"),
			ev(4, true, true, "h0"),
			ev(5, false, true, "h7"),
			ev(6, true, true, "h1"),
			ev(7, false, true, "h3"),
		},
	}
}

func TestSequentialReplayHandStates(t *testing.T) {
	m, cfg := handModel(t)
	out := SequentialReplay(cfg, m, []Match{handDraft()})
	if len(out) != 1 {
		t.Fatalf("got %d replays, want 1", len(out))
	}
	r := out[0]
	if len(r.Picks) != 3 {
		t.Fatalf("got %d pool picks, want 3 (h2, h1, h3)", len(r.Picks))
	}
	h2, h1, h3 := r.Picks[0], r.Picks[1], r.Picks[2]

	// k and a ramps: allies are pool-only (h0 invisible), enemies all picks.
	if h2.K != 0 || h2.A != 0 {
		t.Fatalf("h2 state k=%d a=%d, want 0 0", h2.K, h2.A)
	}
	if h1.K != 2 || h1.A != 1 {
		t.Fatalf("h1 state k=%d a=%d, want 2 1", h1.K, h1.A)
	}
	if h3.K != 3 || h3.A != 0 {
		t.Fatalf("h3 state k=%d a=%d, want 3 0", h3.K, h3.A)
	}

	// hand-computed scores with the constant-0.5 model. h3 is the draft's
	// last event: everything else is taken, so its unseen set is empty and
	// the generic-fit, exposure, and flexibility terms all read 0.
	wantScores := map[int]float64{h2.Idx: 0.90, h1.Idx: 0.52, h3.Idx: 0.55}
	for _, p := range r.Picks {
		if math.Abs(p.Score-wantScores[p.Idx]) > 1e-9 {
			t.Fatalf("pick %s score %v, want %v", m.Slugs[p.Idx], p.Score, wantScores[p.Idx])
		}
	}

	// seq2: n=3, h2 top by prior plus the roleCandidatesGated bonus that only
	// fires under the non-inert baseline (count 0)
	if h2.PoolN != 3 || h2.PoolRank != 1 || h2.Percentile != 1.0 {
		t.Fatalf("h2 rank %d/%d pct %v, want 1/3 pct 1", h2.PoolRank, h2.PoolN, h2.Percentile)
	}
	if rr := h2.RoleRank["1"]; rr.Rank != 1 || rr.N != 2 {
		t.Fatalf("h2 role-1 rank %+v, want {1 2}", rr)
	}

	// seq6: h3 is hard-gated out of the candidate set once h7 is visible, so
	// h1 ranks alone and the percentile is skipped
	if h1.PoolN != 1 || h1.PoolRank != 1 || h1.Ranked() {
		t.Fatalf("h1 rank %d/%d, want the gated field collapsed to 1 with no percentile", h1.PoolRank, h1.PoolN)
	}
	if rr := h1.RoleRank["1"]; rr.Rank != 1 || rr.N != 1 {
		t.Fatalf("h1 role-1 rank %+v, want {1 1}", rr)
	}

	// seq7: h7 sits on h3's own side, so the veto does not fire for h3 itself
	if h3.PoolN != 1 || h3.PoolRank != 1 {
		t.Fatalf("h3 rank %d/%d, want 1/1", h3.PoolRank, h3.PoolN)
	}
	if rr := h3.RoleRank["2"]; rr.Rank != 1 || rr.N != 1 {
		t.Fatalf("h3 role-2 rank %+v, want {1 1}", rr)
	}

	// advantage: radiant 0.90 + 0.52 minus dire 0.55
	if math.Abs(r.Advantage-0.87) > 1e-9 {
		t.Fatalf("advantage %v, want 0.87", r.Advantage)
	}
}

// the shared core field: with roles 1 and 2 pooled, an early h3 pick ranks
// inside the full core field instead of its seat alone, while the role
// agnostic full-pool ranking does not move.
func TestSequentialReplaySharedRoleField(t *testing.T) {
	solo := Match{ID: 2, StartTime: 1000, RadiantWin: true, GameMode: "CAPTAINS_MODE",
		LobbyType: "PRACTICE", Bracket: "TEST", AverageRank: 30,
		Events: []DraftEvent{{Seq: 0, IsRadiant: true, IsPick: true, Slug: "h3", HeroID: 200}}}
	m, cfg := handModel(t)
	cfg.SharedRolePools = nil
	out := SequentialReplay(cfg, m, []Match{solo})
	if rr := out[0].Picks[0].RoleRank["2"]; rr.N != 1 {
		t.Fatalf("unshared role-2 field N = %d, want 1", rr.N)
	}
	m2, cfg2 := handModel(t)
	cfg2.SharedRolePools = [][]string{{"1", "2"}}
	out2 := SequentialReplay(cfg2, m2, []Match{solo})
	p := out2[0].Picks[0]
	if rr := p.RoleRank["2"]; rr.N != 3 || rr.Rank != 3 {
		t.Fatalf("shared role-2 field %+v, want rank 3 of 3 under the priors", rr)
	}
	if p.PoolN != 3 || out[0].Picks[0].PoolN != 3 {
		t.Fatal("full-pool field must not move with the shared seats")
	}
}

// TestSequentialReplayGatedExclusionIsolates the hard gate: without the h3
// veto rule the seq6 candidate field is {h1, h3}, h3 outscores h1, and h1's
// percentile lands at the bottom of a 2-candidate field.
func TestSequentialReplayGatedExclusion(t *testing.T) {
	m, cfg := handModel(t)
	// strip the hard-gate rule, keep the two delta rules
	m.Gates.Rules = []gates.Rule{
		{Id: "t-h1-pen", Target: "h1", When: []gates.Condition{
			{Kind: gates.KindEnemyVisibleAny, Heroes: []string{"h6"}},
		}, Action: gates.ActionPenalty},
		{Id: "t-h2-bon", Target: "h2", When: []gates.Condition{
			{Kind: gates.KindRoleCandidatesGated, Count: 0},
		}, Action: gates.ActionBonus},
	}
	out := SequentialReplay(cfg, m, []Match{handDraft()})
	h1 := out[0].Picks[1]
	if h1.PoolN != 2 || h1.PoolRank != 2 || h1.Percentile != 0.0 {
		t.Fatalf("ungated h1 rank %d/%d pct %v, want 2/2 pct 0", h1.PoolRank, h1.PoolN, h1.Percentile)
	}
}

func TestFinalSetReplayHandStates(t *testing.T) {
	m, cfg := handModel(t)
	out := FinalSetReplay(cfg, m, []Match{handDraft()})
	r := out[0]
	if len(r.Picks) != 3 {
		t.Fatalf("got %d pool picks, want 3", len(r.Picks))
	}
	h2, h1, h3 := r.Picks[0], r.Picks[1], r.Picks[2]
	// final-set states: enemies are every opposing pick, allies the side's
	// other pool picks, the picked hero left out of taken
	if h2.K != 3 || h2.A != 1 {
		t.Fatalf("h2 final state k=%d a=%d, want 3 1", h2.K, h2.A)
	}
	if h1.K != 3 || h1.A != 1 {
		t.Fatalf("h1 final state k=%d a=%d, want 3 1", h1.K, h1.A)
	}
	if h3.K != 3 || h3.A != 0 {
		t.Fatalf("h3 final state k=%d a=%d, want 3 0", h3.K, h3.A)
	}
	// the leave-one-out taken set still empties the unseen field on an
	// 8-hero roster with a full draft, so only the ramp, prior, and gate
	// delta terms survive: h2 0.3+0.07+0.45+0.25, h1 0.3+0.07+0.35-0.25,
	// h3 0.3+0.25
	want := map[int]float64{h2.Idx: 1.07, h1.Idx: 0.47, h3.Idx: 0.55}
	for _, p := range r.Picks {
		if math.Abs(p.Score-want[p.Idx]) > 1e-9 {
			t.Fatalf("final-set %s score %v, want %v", m.Slugs[p.Idx], p.Score, want[p.Idx])
		}
	}
	// advantage: (1.07 + 0.47) - 0.55
	if math.Abs(r.Advantage-0.99) > 1e-9 {
		t.Fatalf("final-set advantage %v, want 0.99", r.Advantage)
	}
	for _, p := range r.Picks {
		if p.PoolN != 1 || p.PoolRank != 1 {
			t.Fatalf("final-set %s rank %d/%d, want the restored 1/1", m.Slugs[p.Idx], p.PoolRank, p.PoolN)
		}
	}
}
