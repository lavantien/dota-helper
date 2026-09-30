// Package config loads and validates config.json, the single config hub.
// Every constant the engine and picker use lives there; code holds no tunables.
package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
)

type RoleDef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type PoolEntry struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
	Tier string `json:"tier"`
}

// Hero merges the pool entries of one slug into a single multi-role hero.
type Hero struct {
	Slug  string
	Name  string
	Roles []string          // ascending
	Tier  map[string]string // role -> tier
}

type ExplorerScope struct {
	RankTierMin int `json:"rankTierMin"`
}

type Scope struct {
	// GameMode and LobbyType are the single hub ids for the product scope
	// (ranked all pick, ranked lobby); consumers map them to each source's
	// representation.
	Bracket   string        `json:"bracket"`
	GameMode  int           `json:"gameMode"`
	LobbyType int           `json:"lobbyType"`
	Explorer  ExplorerScope `json:"explorer"`
	Note      string        `json:"note"`
}

type StratzCfg struct {
	Take              int               `json:"take"`
	RequestIntervalMs int               `json:"requestIntervalMs"`
	BurstCapPerSec    int               `json:"burstCapPerSec"`
	MinWithRows       int               `json:"minWithRows"`
	MinVsRows         int               `json:"minVsRows"`
	MaxAbsSynergy     float64           `json:"maxAbsSynergy"`
	Headers           map[string]string `json:"headers"`
}

type OpenDotaCfg struct {
	RequestsPerMin     int `json:"requestsPerMin"`
	ExplorerMinMatches int `json:"explorerMinMatches"`
}

// BuildsCfg drives the stratz item-build crawl and the derive step that syncs
// content.json build/timings. Values are data-quality knobs, the graphql field
// itself is pinned in the ingest layer.
type BuildsCfg struct {
	MinMatches        int               `json:"minMatches"`        // per-item sample floor for the derived build
	TopN              int               `json:"topN"`              // core build list length
	TimingTopN        int               `json:"timingTopN"`        // timings entries kept
	MinCost           int               `json:"minCost"`           // gold-cost floor, cuts regen and consumables
	ProseContextItems []string          `json:"proseContextItems"` // item names prose may name as context
	ProseGateStopWords []string         `json:"proseGateStopWords"`
	ItemAliases       map[string]string `json:"itemAliases"` // shortName -> compact display token
}

type Thresholds struct {
	DisCut            float64 `json:"disCut"`
	MinKept           int     `json:"minKept"`
	MatchupMinMatches int     `json:"matchupMinMatches"`
	ThinCellMatches   int     `json:"thinCellMatches"`
	AutoseedCap       int     `json:"autoseedCap"`
}

type ShrinkCfg struct {
	MatchupAlpha   float64 `json:"matchupAlpha"`
	SynergyAlpha   float64 `json:"synergyAlpha"`
	OverallWrAlpha float64 `json:"overallWrAlpha"`
}

type CompletionCfg struct {
	Rank       int     `json:"rank"`
	Lambda     float64 `json:"lambda"`
	MaxIter    int     `json:"maxIter"`
	Tol        float64 `json:"tol"`
	Seed       int64   `json:"seed"`
	PivotFloor float64 `json:"pivotFloor"`
}

type Weights struct {
	KnownMu     float64            `json:"knownMu"`
	SynByRole   map[string]float64 `json:"synByRole"` // role id -> known-syn phase weight, authored
	Prior       float64            `json:"prior"`
	GenericFit  float64            `json:"genericFit"`
	Exposure    float64            `json:"exposure"`
	Flexibility float64            `json:"flexibility"`
}

// ScoreConsts holds the structural scoring constants. The JS twin reads the
// same values from the emitted picker data, so they must live in the hub.
type ScoreConsts struct {
	EnemySlots float64 `json:"enemySlots"`
	AllySlots  float64 `json:"allySlots"`
	MidPct     float64 `json:"midPct"`
	FlexCap    int     `json:"flexCap"`
	FlexHalf   float64 `json:"flexHalf"`
}

type NormalizeCfg struct {
	ZSdFloor float64 `json:"zSdFloor"`
}

// BackfillCfg drives the per-match draft crawl. gameMode/lobbyType are the
// probe-verified numeric stratz ids for the accessible per-match source (see
// note), take is clamped to the server cap by the crawler.
type BackfillCfg struct {
	Take           int    `json:"take"`
	MaxRequests    int    `json:"maxRequests"`
	TargetMatches  int    `json:"targetMatches"`
	MinDurationSec int    `json:"minDurationSec"`
	GameMode       int    `json:"gameMode"`
	LobbyType      int    `json:"lobbyType"`
	Note           string `json:"note"`
}

// Eval section: consumed by the eval phase; validated here so a bad value
// fails at load, not mid fit.
type EvalSplitCfg struct{ TrainFrac float64 `json:"trainFrac"` }

type EvalBootstrapCfg struct {
	Resamples int   `json:"resamples"`
	Seed      int64 `json:"seed"`
}

type EvalAlphaFitCfg struct {
	Grid       []float64 `json:"grid"`
	MinHoldoutN int      `json:"minHoldoutN"`
}

type EvalCompletionFitCfg struct {
	Ranks    []int     `json:"ranks"`
	Lambdas  []float64 `json:"lambdas"`
	MaskFrac float64   `json:"maskFrac"`
}

type EvalFitCfg struct {
	GridStep       float64 `json:"gridStep"`
	MaxWeight      float64 `json:"maxWeight"`
	MaxPasses      int     `json:"maxPasses"`
	MinHoldoutGain float64 `json:"minHoldoutGain"`
}

type EvalScreenCfg struct {
	Enabled         bool    `json:"enabled"`
	Q               float64 `json:"q"`
	MinMatches      int     `json:"minMatches"`
	ChiSqMinExpected float64 `json:"chiSqMinExpected"`
	FailAlphaMult   float64 `json:"failAlphaMult"`
}

type EvalNaiveBayesCfg struct{ Alpha float64 `json:"alpha"` }

type EvalKnnCfg struct{ K int `json:"k"` }

type EvalTriplesCfg struct {
	MinSupport int  `json:"minSupport"`
	Enabled    bool `json:"enabled"`
}

// EvalSeqCfg gates the pick-order diagnostics: Enabled is reserved for a
// live seqPrior term, the census knobs drive the report's asymmetry census.
type EvalSeqCfg struct {
	Enabled        bool    `json:"enabled"`
	CensusMinCount int     `json:"censusMinCount"`
	CensusRatio    float64 `json:"censusRatio"`
}

// EvalEnsembleCfg gates the bagged blend: Enabled is reserved for live
// integration, Bags is the report diagnostic's bootstrap replicate count.
type EvalEnsembleCfg struct {
	Enabled bool `json:"enabled"`
	Bags    int  `json:"bags"`
}

type EvalCfg struct {
	TopK            []int                `json:"topK"`
	MinSidePool     int                  `json:"minSidePool"`
	Split           EvalSplitCfg         `json:"split"`
	Bootstrap       EvalBootstrapCfg     `json:"bootstrap"`
	CalibrationBins int                  `json:"calibrationBins"`
	ReportMaxCells  int                  `json:"reportMaxCells"` // 0 keeps every audit cell; positive caps the non-survivor sample
	AlphaFit        EvalAlphaFitCfg      `json:"alphaFit"`
	CompletionFit   EvalCompletionFitCfg `json:"completionFit"`
	Fit             EvalFitCfg           `json:"fit"`
	Screen          EvalScreenCfg        `json:"screen"`
	NaiveBayes      EvalNaiveBayesCfg    `json:"naiveBayes"`
	Knn             EvalKnnCfg           `json:"knn"`
	Triples         EvalTriplesCfg       `json:"triples"`
	Seq             EvalSeqCfg           `json:"seq"`
	Ensemble        EvalEnsembleCfg      `json:"ensemble"`
}

type Paths struct {
	TokenFile        string `json:"tokenFile"`
	DBFile           string `json:"dbFile"`
	StratzRawDir     string `json:"stratzRawDir"`
	OpenDotaRawDir   string `json:"opendotaRawDir"`
	BuildsRawDir     string `json:"buildsRawDir"`
	MatchesRawDir    string `json:"matchesRawDir"`
	PositionsRawDir  string `json:"positionsRawDir"`
	TrendsFile       string `json:"trendsFile"`
	MatchupsDir      string `json:"matchupsDir"`
	PickerDir        string `json:"pickerDir"`
	GuideDir         string `json:"guideDir"`
	ContentPath      string `json:"contentPath"`
	EvalOut          string `json:"evalOut"`
	FitOut           string `json:"fitOut"`
	DerivationsPath  string `json:"derivationsPath"`
}

type EmitCfg struct {
	PickerOut string `json:"pickerOut"`
	GuideOut  string `json:"guideOut"`
	DataJsOut string `json:"dataJsOut"`
	Decimals  int    `json:"decimals"`
}

// HeatmapCfg sizes the guide heatmap. The enemy columns themselves are not
// authored: mine derives them from live pick share, enemyCount is how many
// the emit keeps.
type HeatmapCfg struct {
	EnemyCount int `json:"enemyCount"`
}

type Config struct {
	Patch          string             `json:"patch"`
	PatchEpoch     string             `json:"patchEpoch"`
	Roles          []RoleDef          `json:"roles"`
	SharedRolePools [][]string        `json:"sharedRolePools"` // roles fielding one shared picker candidate pool
	Pool           []PoolEntry        `json:"pool"`
	Heatmap        HeatmapCfg         `json:"heatmap"`
	AoEClearHeroes []string           `json:"aoeClearHeroes"`
	Scope          Scope              `json:"scope"`
	Endpoints      map[string]string  `json:"endpoints"` // api roots, documentation-only consumers read the file directly
	Stratz         StratzCfg          `json:"stratz"`
	OpenDota       OpenDotaCfg        `json:"opendota"`
	Builds         BuildsCfg          `json:"builds"`
	Backfill       BackfillCfg        `json:"backfill"`
	Eval           EvalCfg            `json:"eval"`
	Thresholds     Thresholds         `json:"thresholds"`
	Shrink         ShrinkCfg          `json:"shrink"`
	Completion     CompletionCfg      `json:"completion"`
	Weights        Weights            `json:"weights"`
	Score          ScoreConsts        `json:"score"`
	Normalize      NormalizeCfg       `json:"normalize"`
	GateDeltas     map[string]float64 `json:"gateDeltas"`
	Aliases        map[string]string  `json:"aliases"`
	Paths          Paths              `json:"paths"`
	Emit           EmitCfg            `json:"emit"`
}

// Load reads and validates the config hub at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) Validate() error {
	roleIDs := map[string]bool{}
	for _, r := range c.Roles {
		if roleIDs[r.ID] {
			return fmt.Errorf("duplicate role id %q", r.ID)
		}
		roleIDs[r.ID] = true
	}
	if len(roleIDs) != 5 {
		return fmt.Errorf("want 5 roles, got %d", len(roleIDs))
	}
	if err := c.validateSharedRolePools(roleIDs); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, e := range c.Pool {
		key := e.Slug + "@" + e.Role
		if seen[key] {
			return fmt.Errorf("duplicate pool entry %s", key)
		}
		seen[key] = true
		if !roleIDs[e.Role] {
			return fmt.Errorf("pool entry %s has unknown role %q", e.Slug, e.Role)
		}
		switch e.Tier {
		case "dedicated", "flex":
		default:
			return fmt.Errorf("pool entry %s has unknown tier %q", e.Slug, e.Tier)
		}
	}
	for _, r := range c.Roles {
		n := 0
		for _, e := range c.Pool {
			if e.Role == r.ID {
				n++
			}
		}
		if n < 2 {
			return fmt.Errorf("role %s has %d pool entries, want >= 2", r.ID, n)
		}
	}
	for _, k := range []string{"bonus", "penalty"} {
		if _, ok := c.GateDeltas[k]; !ok {
			return fmt.Errorf("gateDeltas missing %q", k)
		}
	}
	if len(c.Weights.SynByRole) != len(roleIDs) {
		return fmt.Errorf("weights.synByRole must carry exactly the %d role ids, got %d entries", len(roleIDs), len(c.Weights.SynByRole))
	}
	for id := range roleIDs {
		if v, ok := c.Weights.SynByRole[id]; !ok || math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("weights.synByRole role %q missing or not finite", id)
		}
	}
	if c.Stratz.BurstCapPerSec <= 0 || c.Stratz.RequestIntervalMs <= 0 {
		return fmt.Errorf("stratz rate limits must be positive")
	}
	// the winDay family saturates at 30 days server-side (live-verified by
	// make probe-window), so a take at or above it silently pins nothing
	if c.Stratz.Take < 1 || c.Stratz.Take >= 30 {
		return fmt.Errorf("stratz.take must be 1-29 days, %d sits at or above the 30-day server cap and pins no window", c.Stratz.Take)
	}
	if c.Stratz.MaxAbsSynergy <= 0 {
		return fmt.Errorf("stratz.maxAbsSynergy must be positive, 0 would drop every row")
	}
	if c.Builds.MinMatches <= 0 || c.Builds.TopN < 1 || c.Builds.TimingTopN < 1 {
		return fmt.Errorf("builds sample floors and list lengths must be positive")
	}
	if c.Builds.MinCost < 0 {
		return fmt.Errorf("builds.minCost must be >= 0")
	}
	if c.Emit.Decimals <= 0 {
		return fmt.Errorf("emit.decimals must be positive")
	}
	if c.Heatmap.EnemyCount <= 0 {
		return fmt.Errorf("heatmap.enemyCount must be positive")
	}
	if c.Score.EnemySlots <= 0 || c.Score.AllySlots <= 0 || c.Score.FlexHalf <= 0 {
		return fmt.Errorf("score slot and divisor constants must be positive")
	}
	if c.Score.FlexCap < 1 {
		return fmt.Errorf("score.flexCap must be >= 1")
	}
	if c.Score.MidPct <= 0 || c.Score.MidPct >= 1 {
		return fmt.Errorf("score.midPct must be in (0,1)")
	}
	if c.Normalize.ZSdFloor <= 0 {
		return fmt.Errorf("normalize.zSdFloor must be positive")
	}
	if c.Completion.PivotFloor <= 0 {
		return fmt.Errorf("completion.pivotFloor must be positive")
	}
	if c.Thresholds.AutoseedCap < 0 {
		return fmt.Errorf("thresholds.autoseedCap must be >= 0")
	}
	if c.Backfill.Take <= 0 || c.Backfill.MaxRequests <= 0 || c.Backfill.TargetMatches <= 0 || c.Backfill.MinDurationSec <= 0 {
		return fmt.Errorf("backfill take, maxRequests, targetMatches, and minDurationSec must be positive")
	}
	if c.Backfill.GameMode <= 0 || c.Backfill.LobbyType <= 0 {
		return fmt.Errorf("backfill gameMode and lobbyType ids must be positive")
	}
	if c.Scope.GameMode <= 0 || c.Scope.LobbyType <= 0 {
		return fmt.Errorf("scope.gameMode and scope.lobbyType ids must be positive")
	}
	if err := c.Eval.validate(); err != nil {
		return err
	}
	return nil
}

func (e *EvalCfg) validate() error {
	if len(e.TopK) == 0 || !ascendingInts(e.TopK) || e.TopK[0] <= 0 {
		return fmt.Errorf("eval.topK must be nonempty ascending positive ints")
	}
	if e.MinSidePool < 1 {
		return fmt.Errorf("eval.minSidePool must be >= 1")
	}
	if e.Split.TrainFrac <= 0 || e.Split.TrainFrac >= 1 {
		return fmt.Errorf("eval.split.trainFrac must be in (0,1)")
	}
	if e.Bootstrap.Resamples <= 0 {
		return fmt.Errorf("eval.bootstrap.resamples must be positive")
	}
	if e.CalibrationBins < 2 {
		return fmt.Errorf("eval.calibrationBins must be >= 2")
	}
	if e.ReportMaxCells < 0 {
		return fmt.Errorf("eval.reportMaxCells must be >= 0")
	}
	if len(e.AlphaFit.Grid) == 0 || !ascendingFloats(e.AlphaFit.Grid) {
		return fmt.Errorf("eval.alphaFit.grid must be nonempty ascending")
	}
	if e.AlphaFit.MinHoldoutN < 1 {
		return fmt.Errorf("eval.alphaFit.minHoldoutN must be >= 1")
	}
	if len(e.CompletionFit.Ranks) == 0 || !ascendingInts(e.CompletionFit.Ranks) {
		return fmt.Errorf("eval.completionFit.ranks must be nonempty ascending")
	}
	if len(e.CompletionFit.Lambdas) == 0 || !ascendingFloats(e.CompletionFit.Lambdas) || e.CompletionFit.Lambdas[0] <= 0 {
		return fmt.Errorf("eval.completionFit.lambdas must be nonempty ascending positive")
	}
	if e.CompletionFit.MaskFrac <= 0 || e.CompletionFit.MaskFrac >= 1 {
		return fmt.Errorf("eval.completionFit.maskFrac must be in (0,1)")
	}
	if e.Fit.GridStep <= 0 || e.Fit.MaxWeight <= 0 || e.Fit.MaxPasses < 1 || e.Fit.MinHoldoutGain < 0 {
		return fmt.Errorf("eval.fit floors must be positive and maxPasses >= 1")
	}
	if e.Screen.Q <= 0 || e.Screen.Q >= 1 {
		return fmt.Errorf("eval.screen.q must be in (0,1)")
	}
	if e.Screen.MinMatches < 1 || e.Screen.ChiSqMinExpected <= 0 || e.Screen.FailAlphaMult < 1 {
		return fmt.Errorf("eval.screen floors must hold: minMatches >= 1, chiSqMinExpected > 0, failAlphaMult >= 1")
	}
	if e.NaiveBayes.Alpha <= 0 {
		return fmt.Errorf("eval.naiveBayes.alpha must be positive")
	}
	if e.Knn.K <= 0 {
		return fmt.Errorf("eval.knn.k must be positive")
	}
	if e.Triples.MinSupport < 1 {
		return fmt.Errorf("eval.triples.minSupport must be >= 1")
	}
	if e.Seq.CensusMinCount < 1 {
		return fmt.Errorf("eval.seq.censusMinCount must be >= 1")
	}
	// at ratio 1 every two-sided pair reads asymmetric (hi >= lo always),
	// so the census needs a strictly material threshold
	if e.Seq.CensusRatio <= 1 {
		return fmt.Errorf("eval.seq.censusRatio must exceed 1")
	}
	if e.Ensemble.Bags < 1 {
		return fmt.Errorf("eval.ensemble.bags must be >= 1")
	}
	return nil
}

func ascendingInts(v []int) bool {
	for i := 1; i < len(v); i++ {
		if v[i] <= v[i-1] {
			return false
		}
	}
	return true
}

func ascendingFloats(v []float64) bool {
	for i := 1; i < len(v); i++ {
		if v[i] <= v[i-1] {
			return false
		}
	}
	return true
}

// Heroes merges pool entries into unique multi-role heroes, slug ascending.
func (c *Config) Heroes() []Hero {
	bySlug := map[string]*Hero{}
	var order []string
	for _, e := range c.Pool {
		h, ok := bySlug[e.Slug]
		if !ok {
			h = &Hero{Slug: e.Slug, Name: e.Name, Tier: map[string]string{}}
			bySlug[e.Slug] = h
			order = append(order, e.Slug)
		}
		h.Roles = append(h.Roles, e.Role)
		h.Tier[e.Role] = e.Tier
	}
	sort.Strings(order)
	out := make([]Hero, 0, len(order))
	for _, s := range order {
		h := bySlug[s]
		sort.Strings(h.Roles)
		out = append(out, *h)
	}
	return out
}

// HeroBySlug returns the merged hero or nil.
func (c *Config) HeroBySlug(slug string) *Hero {
	for _, h := range c.Heroes() {
		if h.Slug == slug {
			return &h
		}
	}
	return nil
}

// PoolSlugs returns unique pool slugs, slug ascending.
func (c *Config) PoolSlugs() []string {
	hs := c.Heroes()
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Slug
	}
	return out
}

// NPCPrefix is the dota internal hero npc prefix, e.g. npc_dota_hero_axe.
const NPCPrefix = "npc_dota_hero_"

var seriesTailRx = regexp.MustCompile(`[a-zA-Z]+$`)

// PatchSeries strips the letter suffix: 7.41f -> 7.41. Rolling aggregates and
// patch-scoped scrapes compare series, not letter patches.
func PatchSeries(patch string) string {
	return seriesTailRx.ReplaceAllString(patch, "")
}

// ShortNPC strips the npc prefix and keeps the remainder verbatim, including
// underscores and legacy names (windrunner, necrolyte, furion). That is the
// file name the steam CDN serves hero icons under; SlugFromNPC output is not,
// it hyphenates and alias-resolves.
func ShortNPC(npc string) string {
	if len(npc) > len(NPCPrefix) && npc[:len(NPCPrefix)] == NPCPrefix {
		return npc[len(NPCPrefix):]
	}
	return npc
}

// SlugFromNPC strips the npc prefix, underscores become hyphens, then the
// alias map resolves legacy names.
func (c *Config) SlugFromNPC(npc string) string {
	s := strings.ReplaceAll(ShortNPC(npc), "_", "-")
	if v, ok := c.Aliases[s]; ok {
		return v
	}
	return s
}

var slugRx = regexp.MustCompile(`^[a-z0-9-]+$`)

// ValidSlug reports whether s is a safe roster slug: lowercase letters, digits,
// hyphens, nothing else. Roster slugs become cache file names and data columns
// downstream, so anything else must fail loudly at the roster boundary.
func ValidSlug(s string) bool {
	return slugRx.MatchString(s)
}
