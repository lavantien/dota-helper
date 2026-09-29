package ingest

import (
	"fmt"
	"sort"
	"strings"
)

// Live schema discovery for the stratz matches root field. docs.stratz.com is
// Cloudflare-gated and the API serves null for introspection, so the schema
// facts the crawler pins come from what these probes print: a best-effort
// __schema read plus an accept/reject ladder over candidate arguments.

// One batched __schema read: stratz serves null for named __type lookups, so
// the full type list is fetched once and the interesting types picked out
// client-side.
const probeIntrospectionQuery = `query {
  sch: __schema {
    queryType { name fields { name args { name type { kind name ofType { kind name ofType { kind name } } } } type { kind name ofType { kind name ofType { kind name } } } } }
    types { name fields { name args { name type { kind name ofType { kind name ofType { kind name } } } } type { kind name ofType { kind name ofType { kind name } } } } inputFields { name type { kind name ofType { kind name ofType { kind name } } } } enumValues { name } }
  }
}`

type introField struct {
	Name string        `json:"name"`
	Type *introTypeRef `json:"type,omitempty"`
}

type introTypeRef struct {
	Kind   string        `json:"kind"`
	Name   string        `json:"name"`
	OfType *introTypeRef `json:"ofType"`
}

// displayName unwraps NON_NULL/LIST wrappers to the named type.
func (r *introTypeRef) displayName() string {
	if r == nil {
		return "?"
	}
	if r.Name != "" {
		return r.Name
	}
	return r.OfType.displayName()
}

type introFieldFull struct {
	Name string        `json:"name"`
	Args []introField  `json:"args"`
	Type *introTypeRef `json:"type"`
}

type introType struct {
	Name        string           `json:"name"`
	Fields      []introFieldFull `json:"fields"`
	InputFields []introField     `json:"inputFields"`
	EnumValues  []introField     `json:"enumValues"`
}

// introEnvelope mirrors decodeEnvelope semantics: the out value receives the
// inner data object, so the alias key sits at the top level here.
type introEnvelope struct {
	Sch struct {
		QueryType *introType   `json:"queryType"`
		Types     []*introType `json:"types"`
	} `json:"sch"`
}

// matchesSchema holds what live discovery established. Empty maps mean
// nothing resolved: callers fall back to the conventional field set and the
// shape ladder alone decides.
type matchesSchema struct {
	args    map[string]bool // root matches() argument names
	fields  map[string]bool // Match selection field names
	filters map[string]bool // MatchQuery input field names
	enums   map[string][]string
}

// fetchRootMatches decodes the root matches(ids:) variant used by the
// discovery ladder and the by-ids round trip.
func fetchRootMatches(c *stratzClient, q string) ([]probeMatch, error) {
	body, err := c.query(q)
	if err != nil {
		return nil, err
	}
	var d struct {
		Matches []probeMatch `json:"matches"`
	}
	if err := decodeEnvelope(body, &d); err != nil {
		return nil, err
	}
	return d.Matches, nil
}

// discoverMatchesShape runs the acceptance ladder for the matches root field:
// a zero-argument browse first (harvests one sample match), then one
// candidate argument at a time so the printed accept/reject table is
// per-argument evidence. Returns whatever rows the zero-arg browse served.
func discoverMatchesShape(c *stratzClient, sel string) []probeMatch {
	rows, err := fetchRootMatches(c, fmt.Sprintf("query { matches { %s } }", sel))
	if err != nil {
		fmt.Printf("probe matches zero-arg browse: REJECTED %s\n", shortErr(err))
	} else {
		fmt.Printf("probe matches zero-arg browse: ANSWERS, %d rows\n", len(rows))
	}
	seed := edgeMatch(rows, false).ID
	for _, arg := range []string{
		fmt.Sprintf("ids: [%d]", seed),
		"take: 5",
		"skip: 5",
		"limit: 5",
		"query: { take: 5 }",
	} {
		if seed == 0 && strings.HasPrefix(arg, "ids") {
			continue
		}
		if _, err := fetchRootMatches(c, fmt.Sprintf("query { matches(%s) { id } }", arg)); err != nil {
			fmt.Printf("probe matches arg %q: REJECTED %s\n", arg, shortErr(err))
			continue
		}
		fmt.Printf("probe matches arg %q: ACCEPTED\n", arg)
	}
	return rows
}

// introspectMatches reads the batched schema snapshot, best effort: a refused
// or empty read is not fatal, the arg ladder carries the discovery.
func introspectMatches(c *stratzClient) (matchesSchema, error) {
	body, err := c.query(probeIntrospectionQuery)
	if err != nil {
		return matchesSchema{}, err
	}
	var env introEnvelope
	if err := decodeEnvelope(body, &env); err != nil {
		return matchesSchema{}, err
	}
	fmt.Printf("probe matches: introspection decoded body=%dB queryType=%v types=%d\n",
		len(body), env.Sch.QueryType != nil, len(env.Sch.Types))
	s := matchesSchema{
		args:    map[string]bool{},
		fields:  map[string]bool{},
		filters: map[string]bool{},
		enums:   map[string][]string{},
	}
	if root := env.Sch.QueryType; root != nil {
		var sigs []string
		for _, f := range root.Fields {
			var args []string
			for _, a := range f.Args {
				args = append(args, a.Name)
			}
			sigs = append(sigs, fmt.Sprintf("%s(%s): %s", f.Name, strings.Join(args, ","), f.Type.displayName()))
			if f.Name == "matches" {
				for _, a := range f.Args {
					s.args[a.Name] = true
				}
			}
		}
		fmt.Printf("probe matches: root type %q fields: %v\n", root.Name, sigs)
		fmt.Printf("probe matches: root matches() args %v\n", sortedKeys(s.args))
	} else {
		fmt.Printf("probe matches: introspection body head: %s\n", shortBody(body))
	}
	byName := map[string]*introType{}
	for _, t := range env.Sch.Types {
		byName[t.Name] = t
	}
	if m := byName["MatchType"]; m != nil {
		for _, f := range m.Fields {
			s.fields[f.Name] = true
		}
		var names []string
		for _, f := range m.Fields {
			names = append(names, f.Name)
		}
		fmt.Printf("probe matches: MatchType fields (%d): %v\n", len(names), names)
		for _, want := range []string{"pickBans", "bracket", "lobbyType", "gameMode", "averageRank", "players"} {
			for _, f := range m.Fields {
				if f.Name == want {
					fmt.Printf("probe matches: MatchType.%s : %s\n", want, f.Type.displayName())
					if t := byName[f.Type.displayName()]; t != nil {
						var sub []string
						for _, sf := range t.Fields {
							sub = append(sub, sf.Name)
						}
						fmt.Printf("probe matches: %s fields: %v\n", f.Type.displayName(), sub)
					}
				}
			}
		}
	}
	if sa := byName["SteamAccountType"]; sa != nil {
		var names []string
		for _, f := range sa.Fields {
			if strings.Contains(strings.ToLower(f.Name), "match") {
				var args []string
				for _, a := range f.Args {
					args = append(args, a.Name)
				}
				names = append(names, fmt.Sprintf("%s(%s): %s", f.Name, strings.Join(args, ","), f.Type.displayName()))
			}
		}
		fmt.Printf("probe matches: SteamAccountType match-carrying fields: %v\n", names)
	}
	if pt := byName["PlayerType"]; pt != nil {
		var names []string
		for _, f := range pt.Fields {
			if strings.Contains(strings.ToLower(f.Name), "match") {
				var args []string
				for _, a := range f.Args {
					args = append(args, a.Name)
				}
				names = append(names, fmt.Sprintf("%s(%s): %s", f.Name, strings.Join(args, ","), f.Type.displayName()))
			}
		}
		fmt.Printf("probe matches: PlayerType match-carrying fields: %v\n", names)
	}
	if lb := byName["LeaderboardQuery"]; lb != nil {
		var names []string
		for _, f := range lb.Fields {
			var args []string
			for _, a := range f.Args {
				args = append(args, a.Name)
			}
			names = append(names, fmt.Sprintf("%s(%s): %s", f.Name, strings.Join(args, ","), f.Type.displayName()))
		}
		fmt.Printf("probe matches: LeaderboardQuery fields: %v\n", names)
	}
	for _, n := range []string{"PlayerMatchesRequestType", "PlayerPerformanceMatchesRequestType", "PagePlayerQuery", "PageMatchesQuery", "FilterSeasonLeaderboardRequestType"} {
		if t := byName[n]; t != nil {
			var names []string
			for _, f := range t.InputFields {
				names = append(names, f.Name)
			}
			fmt.Printf("probe matches: input %s: %v\n", n, names)
		}
	}
	if t := byName["SteamAccountSeasonActiveLeaderboardType"]; t != nil {
		var names []string
		for _, f := range t.Fields {
			names = append(names, fmt.Sprintf("%s: %s", f.Name, f.Type.displayName()))
			if f.Name == "players" {
				if pt := byName[f.Type.displayName()]; pt != nil {
					var sub []string
					for _, sf := range pt.Fields {
						sub = append(sub, sf.Name)
					}
					fmt.Printf("probe matches: %s fields: %v\n", f.Type.displayName(), sub)
				}
			}
		}
		fmt.Printf("probe matches: SteamAccountSeasonActiveLeaderboardType fields: %v\n", names)
	}
	for _, n := range []string{"LeaderboardDivision", "FindMatchPlayerOrderBy", "FindMatchPlayerList", "RankBracket", "RankBracketBasicEnum"} {
		t := byName[n]
		if t == nil {
			continue
		}
		var names []string
		for _, v := range t.EnumValues {
			names = append(names, v.Name)
		}
		if len(names) > 0 {
			fmt.Printf("probe matches: enum %s: %v\n", n, names)
			continue
		}
		for _, f := range t.InputFields {
			names = append(names, f.Name)
		}
		if len(names) > 0 {
			fmt.Printf("probe matches: input %s: %v\n", n, names)
		}
	}
	if t := byName["StratzQuery"]; t != nil {
		var names []string
		for _, f := range t.Fields {
			names = append(names, f.Name)
		}
		fmt.Printf("probe matches: StratzQuery fields: %v\n", names)
	}
	if t := byName["LiveType"]; t != nil {
		var names []string
		for _, f := range t.Fields {
			names = append(names, f.Name)
		}
		fmt.Printf("probe matches: LiveType fields: %v\n", names)
	}
	if t := byName["LeagueType"]; t != nil {
		var names []string
		for _, f := range t.Fields {
			if strings.Contains(strings.ToLower(f.Name), "match") || f.Name == "id" || f.Name == "name" {
				var args []string
				for _, a := range f.Args {
					args = append(args, a.Name)
				}
				names = append(names, fmt.Sprintf("%s(%s): %s", f.Name, strings.Join(args, ","), f.Type.displayName()))
			}
		}
		fmt.Printf("probe matches: LeagueType match-carrying fields: %v\n", names)
	}
	for _, n := range []string{"LeagueMatchesRequestType", "LeagueRequestType"} {
		if t := byName[n]; t != nil {
			var names []string
			for _, f := range t.InputFields {
				names = append(names, f.Name)
			}
			fmt.Printf("probe matches: input %s: %v\n", n, names)
		}
	}
	for key, names := range map[string][]string{
		"gameMode":  {"GameModeEnumType", "GameModeEnum"},
		"lobbyType": {"LobbyTypeEnum", "LobbyTypeType"},
	} {
		for _, n := range names {
			if t := byName[n]; t != nil && len(t.EnumValues) > 0 {
				for _, v := range t.EnumValues {
					s.enums[key] = append(s.enums[key], v.Name)
				}
				break
			}
		}
		if len(s.enums[key]) > 0 {
			fmt.Printf("probe matches: %s enum values %v\n", key, s.enums[key])
		}
	}
	if len(s.args) == 0 {
		return s, fmt.Errorf("no matches root field args introspected")
	}
	return s, nil
}

func shortErr(err error) string {
	return shortBody([]byte(err.Error()))
}

func shortBody(b []byte) string {
	const keep = 600
	if len(b) > keep {
		return string(b[:keep]) + "..."
	}
	return string(b)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func intersectSorted(m map[string]bool, want []string) []string {
	var out []string
	for _, k := range want {
		if m[k] {
			out = append(out, k)
		}
	}
	return out
}
