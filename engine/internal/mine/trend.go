package mine

import "sort"

// TrendRow is one dated per-hero snapshot from the committed trends ledger
// (ref/dota2/trends/snapshots.ndjson, rebuilt into hero_trend at ingest).
type TrendRow struct {
	Date      string
	Slug      string
	Share, WR float64
}

// TrendDelta is the latest-pair movement for one pool hero: the change in
// overall win rate and pick share in percentage points between the two most
// recent distinct snapshot dates. Only the latest pair compares, so old
// snapshots never move the emitted number.
type TrendDelta struct {
	WrDeltaPP    float64
	ShareDeltaPP float64
	FromDate     string
	ToDate       string
}

// Trends computes the latest-pair delta per pool hero. Heroes with fewer
// than two snapshot dates are absent.
func Trends(pool []string, rows []TrendRow) map[string]TrendDelta {
	type snap struct {
		date      string
		share, wr float64
	}
	byDate := map[string]map[string]snap{}
	for _, r := range rows {
		if byDate[r.Date] == nil {
			byDate[r.Date] = map[string]snap{}
		}
		byDate[r.Date][r.Slug] = snap{date: r.Date, share: r.Share, wr: r.WR}
	}
	dates := make([]string, 0, len(byDate))
	for d := range byDate {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	out := map[string]TrendDelta{}
	if len(dates) < 2 {
		return out
	}
	prev, last := byDate[dates[len(dates)-2]], byDate[dates[len(dates)-1]]
	for _, slug := range pool {
		a, okA := prev[slug]
		b, okB := last[slug]
		if !okA || !okB {
			continue
		}
		out[slug] = TrendDelta{
			WrDeltaPP:    (b.wr - a.wr) * 100,
			ShareDeltaPP: (b.share - a.share) * 100,
			FromDate:     a.date,
			ToDate:       b.date,
		}
	}
	return out
}
