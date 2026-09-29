# dota 2 reference data

Research data and findings scraped or verified during QnA sessions, kept here so related questions reuse it instead of re-digging.

## layout

- `patch/`: patch series changelog facts (7.41 series)
- `systems/`: game-wide systems (MMR reset, neutral item crafting)
- `items/`: item reference (new and newly relevant items)
- `heroes/`: hero reference (new or unfamiliar heroes)
- `meta/`: meta and tier list snapshots, dated
- `matchups/`: Dotabuff counter tables, one file per hero, scrape-dated
- `matchups/raw/`: verbatim page dumps that scripts/fetch-matchups.ps1 parses
- `matchups/archive-dotabuff/`: superseded snapshots kept for history (raw verbatim dumps)
- `synergy/raw/`: STRATZ graphql crawl caches, one json per hero plus `_roster.json` (see below)
- `builds/raw/`: STRATZ item-build crawl caches, one json per pool entry (hero@role) plus `_items.json` and `_crawl.json` (see below)
- `matches/raw/`: STRATZ per-match draft backfill for leagues, append-only `matches.ndjson` plus `_backfill.json` and `_leagues.json` (see below)
- `howdoiplay/`: Tsunami's hero tips and counters snapshot for the pool heroes (see below)

## howdoiplay conventions

Source: `https://howdoiplay.com/tips/<tip_slug>.html`, patch-scoped static html crawled by `scripts/fetch-howdoiplay.ps1` (run via `make fetch-howdoiplay`). The homepage embeds the live `global.HEROES` roster and a `global.PATCH` stamp; pool hero names must match that roster exactly (case-sensitive), a mismatch throws with the diff instead of silently skipping, and there is deliberately no alias table. The tip slug mirrors the site's own main.js rule: lowercase, spaces to underscores, hyphens and apostrophes kept (`primal_beast`, `nature%27s_prophet`).

Files: `raw/<slug>.html` are verbatim page dumps (repo slugs as filenames, base64 portraits included, ~8KB each) plus `raw/_heroes.json` (roster snapshot) and `raw/_crawl.json` (per-hero fetch metadata). `howdoiplay.json` is the derived extraction: per hero a `tips` and a `counters` array of `{text, notes}`, where top-level `li` entries become `text` and their nested `li` elaborations become `notes`. The loose html (unclosed `li` around nested lists) is parsed by a depth state machine guarded by a 90% word-coverage invariant, so silent text loss fails the run.

No archive tier: pages are static and re-fetchable, so superseded patch snapshots are simply overwritten. `make check` gates the hero set against the config pool and the patch series against the config patch (letter lag tolerated, series jump fails), so rerun the crawl when the patch series moves and re-review any curated lines derived from it.

## synergy crawl conventions

Source: `https://api.stratz.com/graphql`, `heroStats.matchUp(heroId: <id>, take: 200, bracketBasicIds: [DIVINE_IMMORTAL], week: <last completed week start>)`, per the user's api token stored at `var/stratz.token` (gitignored, never committed). Scope: divine-immortal bracket only; the mode scoping picture for every heroStats query is in "scope conventions" below, the window picture in "window conventions". `take` here is PAGINATION (a row cap, 200 covers every enemy row), not a window; the pair tables pin the last completed calendar week via `week`, so the numbers are that week's data, not patch pure; recorded as such in provenance and the synergy `window_note`.

Each `<slug>.json` holds `{heroId, slug, name, npc, fetchedAt, bracket, week, vs, with}`. `vs` rows `{heroId2, matchCount, winCount, synergy}` are enemy matchups, `with` rows are ally synergies where `synergy` is a percentage-point delta (positive = pair overperforms, computed by STRATZ as an expectation-adjusted delta). The row with `heroId 0` is a total row, skip it. STRATZ is the exclusive scoring source for pairs: dotabuff retired from scoring on 2026-09-29 (its dumps stay on disk as frozen reference, ingest purges any `matchup_raw` rows carrying its source label), and nothing currently ingests OpenDota explorer pair rows either, so uncovered pairs read missing and the shrinkage prior plus completion fill them.

## matches backfill conventions

Source: `https://api.stratz.com/graphql`, `league(id) { matches(request: { gameModeIds: [2], startDateTime: <epoch>, endDateTime: <now>, take: 100, skip: <n> }) { ... pickBans ... players ... } }`, pinned by live probe on 2026-09-26 (`make probe-matches`). Per-match pub data is token-gated on the default tier: the root `matches` field accepts `ids` only and answers `User is not an admin`, `match(id:)` serves nothing, and `player(steamAccountId) { matches(request) }` validates but returns empty rows for every shape (take/orderBy/playerList/cursor, singular and plural roots) while `matchCount` on the same player answers fine. League matches are the one per-match source this token reaches, and they carry full drafts: `MatchType.pickBans` rows `{isPick, heroId, order, isRadiant}` (the old `draftTimings` field no longer exists) plus per-player `lane`, `position`, `role`, `roleBasic` for role-conditioned metrics.

Filter arguments on the matches request types are numeric (`gameModeIds: [Byte]`, `lobbyTypeIds: [Byte]`, `startDateTime`/`endDateTime` single Longs; `bracketBasicIds` exists only on the heroStats aggregates, the league request takes `bracketIds: [Int]` with undocumented numbering so bracket scope is gated client-side instead). Enum ids verified against `constants`: gameMode 2 = CAPTAINS_MODE (22 = ALL_PICK_RANKED, what the rows of a ranked-all-pick filter would carry), lobbyType 1 = PRACTICE (2 = TOURNAMENT, 7 = RANKED). Take is capped server-side at 100. Coverage measured on the first 5 leagues in the 7.41f window: 325 in-window matches, 75-88 percent with complete 10-pick drafts, all CAPTAINS_MODE/PRACTICE.

Files: `matches/raw/matches.ndjson` is the append-only reduced cache, one row per line `{matchId, startDateTime, radiantWin, durationSeconds, lobbyType, gameMode, averageRank, leagueId, draft: [{seq, isRadiant, isPick, heroId}], players: [{heroId, isRadiant, lane, position, role, roleBasic}]}` (lane/role fields pass through verbatim, string or int as served). `matches/raw/_backfill.json` is the resumable manifest (league index, last skip, kept/dropped with reason buckets, request budget spent, done flag) and `matches/raw/_leagues.json` caches the paginated league list. Reruns dedupe on match id, never rewrite lines, and the acceptance gates (complete 10-pick draft with unique heroes, duration floor, patch window) drop rows with counted reasons instead of failing. A corrupt committed line fails the crawl loudly; a torn trailing line from a crash mid-append is repaired (fragment dropped, complete row re-terminated) so appends never glue onto it.

A `-refresh` run re-opens a done manifest: the window end extends to now, the leagues cache re-lists, paging restarts at the head of every live league (dedupe keeps committed matches unique), the drop and page counters reset, kept stays cumulative, and the corpus target never stops the walk (it would no-op the window extension since cumulative kept counts committed matches). The re-walk cost grows monotonically with the window: every refresh re-pages every live league from skip 0, and no stage tracks the api's hourly bucket across the pipeline, so a large enough window eats 429 backoffs and can abort the walk; the manifest checkpoints survive and `make fetch-matches` resumes. A per-league high-water mark is the known fix if the window outgrows one run's budget.



## scope conventions (game mode / lobby / position filters)

Verified live 2026-09-27 by `make probe-scope` (schema introspection plus an accept/reject ladder with value diffs; full evidence in the probe output, re-runnable anytime).

1. `gameModeIds` exists ONLY on the win* family (`winHour`, `winDay`, `winWeek`, `winMonth`, `winGameVersion`) and takes enum tokens: `gameModeIds: [ALL_PICK_RANKED]` answers, numeric `[22]` is rejected (`Expected type GameModeEnumType`). `lobbyTypeIds` is accepted nowhere: no heroStats query can be lobby-scoped.
2. `matchUp`, `heroVsHeroMatchup`, `stats`, `itemFullPurchase`, `laneOutcome`, `talent`, `banDay` take NO mode arg (introspected arg lists definitive, both numeric and enum forms Unknown-argument). Pair tables and item builds cannot be mode-scoped on STRATZ at any token tier: the arguments do not exist in the schema.
3. The win* fields apply filters to VALUES while their row-count universe stays fixed (winDay serves 127 x take rows, 3810 at the 30-day cap and 889 at the product's take 7, filtered or not), so equal row counts are not evidence a filter was ignored; the per-hero value diff is the test, and it shows every filter genuinely bites.
4. Measured non-RAP share inside `bracketIds: [DIVINE, IMMORTAL]` (value diff over all 127 heroes, measured at the 30-day cap): winDay median 0.157 max 0.300, winWeek median 0.200 max 0.337, winMonth all-time median 0.226 max 0.327. The divine-immortal bracket aggregate is NOT mode-pure; the mode filter matters.
5. `winGameVersion` rows carry `(gameVersionId, heroId, durationMinute, winCount, matchCount)` under the full bracket+mode+position filter set: per-hero data can be pinned to the exact patch window by gameVersionId. `positionIds` slices (row count 13039 to 12491 at POSITION_1, per-hero values differ across positions) but never tags rows: per-position data costs 5 slice requests. The win* row types expose no position field, so `groupBy: HERO_ID_POSITION_BRACKET` rows are not position-attributable; `groupBy: HERO_ID` left row counts unchanged and is treated as inert.
6. The old popularity ladder arg `bracketBasicIds` is now INVALID on the win* fields ("Did you mean bracketIds") and `winBracket` no longer exists, which is why the committed `_popularity.json` fell through to bracket-ALL `winDay`: the bracket-arg'd ladder rungs have been silently failing.
7. `matchUp` takes no `positionIds` either (args: bracketBasicIds, heroId/heroIds, matchLimit, orderBy, skip, take, week). `findMatchPlayer` does not exist on the DotaQuery root.

Design consequence: per-hero data (overall wr, popularity, positions, trends) can be exactly divine-immortal ranked all pick via the win* family; pair tables (mu/syn) and builds stay `bracketBasicIds: [DIVINE_IMMORTAL]`-scoped with the measured 16-33 percent non-RAP minority documented, unless pairs move off STRATZ, which the standing source policy forbids.

## window conventions (the seven day pin)

Verified live 2026-09-29 by `make probe-window` (introspected arg lists plus take ladders, week-stamp ladders, and cell-subset checks; full evidence in the probe output, re-runnable anytime). The hub pins `stratz.take: 7`: every live aggregate is at most the last 7 days.

1. winDay `take` = days, saturating at 30 server-side (take 200 and take 30 return identical rows and totals). Popularity, positions, and trends roll this window: 889 rows = 127 heroes x 7 at the pin, 3810 = 127 x 30 at the cap.
2. matchUp `take` = PAGINATION, a row cap (take 30 serves exactly 30 rows, take 200 serves all 126 enemies), never a window. Its `week` arg (Long) is a calendar-week stamp at week-start unix seconds, Monday 00:00 UTC: a completed week's stamp answers that week's data, and an in-progress week's stamp is ignored server-side (falls back to an undocumented rolling default). The crawl pins `week` to the last completed week start.
3. itemFullPurchase shares the `week` stamp and behaves the same way; its `minTime`/`maxTime` (Int) filter purchase minutes inside a match, not the window (44 of 366 rows at minutes 0-10). Builds are display/prose only and never scored, so they ride the week pin as a documented exception rather than a scored-window claim.
4. Guards against mixed windows: every raw cache stamps its window (`rawCache.week`, `popularityFile.take`, `positionsFile.take`, `buildsCache.week`, crawl manifests), ingest aborts loudly on take mismatches, unstamped legacy caches, and mixed calendar weeks across pool heroes, and resumable caches are only reused when their week equals the pinned week. The trends ledger carries a `window` field per line (legacy unstamped lines attribute 30) and `hero_trend` projects the latest (scope, window) series only, so deltas never straddle a window cut.

## positions and trends conventions

Positions: `heroStats.winDay(take, bracketIds: [DIVINE, IMMORTAL], gameModeIds: [ALL_PICK_RANKED], positionIds: [POSITION_k])`, one request per farm position k=1..5 (`make fetch-positions`, 5 requests total). Rows are take-day day buckets (7 at the hub pin) folded per hero into `ref/dota2/positions/raw/_positions.json` and the `hero_position` table. Display only: the picker payload carries `heroPos` per pool hero as [position, share of the hero's own picks, wr] triples, never scored.

Trends: `ref/dota2/trends/snapshots.ndjson` is the committed append-only ledger, one line per (crawl date, hero) `{date, scope, field, window, slug, pick_share, wr}` from the same scoped `_popularity.json` aggregate, `window` stamped from its take (7 since 2026-09-29; legacy unstamped lines read as the pre-cut 30-day window). The first crawl of a day wins per window (same-day re-crawls of the same window append nothing), `hero_trend` is the rebuilt projection of the latest (scope, window) series, and mine derives `hero_trend_delta` as that series' latest-pair movement (wr and pick share in pp between the two most recent distinct dates, old snapshots never move the number, no delta ever straddles the window cut). History legitimately starts 2026-09-26 UTC (2026-09-27 +0700 local, the stamp of the first scoped crawl): earlier snapshots would be cross-scope (all-bracket, all-mode) and their deltas meaningless. Deltas render as display-only chips (`trend` in the picker payload, `trend`/`trendWindow` in the guide artifact); the 7-day series starts 2026-09-29, so its chips stay empty until the second 7-day snapshot.

## builds crawl conventions

Source: `https://api.stratz.com/graphql`, `heroStats.itemFullPurchase(heroId: <id>, bracketBasicIds: [DIVINE_IMMORTAL], positionIds: [POSITION_<1-5>], matchLimit: 1, week: <last completed week start>)` plus `constants.items` for names, costs, and recipe component lists. Field names and row shape were pinned by live introspection on 2026-09-25 (`HeroStatsQuery` -> `itemFullPurchase` returns `HeroItemPurchaseType`): rows are per `(itemId, time, instance)` purchase counts where `time` is the purchase minute bucket and `instance` 0 is the first copy bought. `matchLimit` is a server-side per-row sample floor (without it, thin position slices return zero rows), 1 disables it so the derive step can apply the config hub floor instead. The `week` stamp pins the completed calendar week (see "window conventions"; `minTime`/`maxTime` filter purchase minutes inside a match, not the window), so these are week-pinned counts, not patch pure; the manifest records the patch series, not the letter, for the same reason.

Files: `raw/<slug>@<role>.json` per pool entry `{slug, role, heroId, fetchedAt, bracket, week, rows}`, one crawl per farm position because position slices are how multi-role heroes get honest builds (abaddon support vs core). `raw/_items.json` is the items constants anchor, `raw/_crawl.json` the resumable manifest (carrying `week`, `windowNote`, and `series`; the derive refuses a manifest not stamped with the current completed week). Recipe items stay in the constants dump: `recipe_<x>` shortNames plus their `components { componentId }` lists are what let the derive step suppress components (yasha, oblivion staff) of popular final items (manta, orchid).

## matchups data conventions

Source: `https://de.dotabuff.com/heroes/<slug>/counters` (German locale subdomain), month filter, scraped 2026-09-24 during patch 7.41f. These 18 dumps cover the PREVIOUS pool. They served as the backup matchup tier from 2026-09-27 and retired from scoring on 2026-09-29 with the STRATZ-exclusive source policy: nothing parses them anymore, ingest actively purges any `matchup_raw` rows carrying the dotabuff source label, and pairs the stratz crawl does not cover read missing and fill from the shrinkage prior plus the fitted completion. The files stay on disk as frozen reference (a fixed month window under a superseded pool); the column semantics below document what they hold. All 18 were pulled from the same subdomain in one pass so every number shares one live window.

Columns per row: `dis%` is Dotabuff's disadvantage number for the file hero against the listed enemy. Positive = the enemy listed beats the file hero, negative = the file hero gains. The baseline Dotabuff normalizes against is not documented and does not reconstruct as a constant offset from `wr%` (the implied baseline varies row to row), so treat the sign as reliable and magnitudes as comparable within one table rather than across columns. `wr%` is the file hero's raw winrate in that matchup. `matches` is the sampled game count, use it to weight confidence.

Why the German locale: the English pages for 9 of the 18 pool heroes only ever serve stale CDN snapshots to any fetcher available on this machine (stamps 2024-07 to 2025-06), Windranger's English page additionally serves a Cloudflare bot challenge, and the English pages that did load carried an August 2026 window that had drifted up to 4.4 points and one sign flip against the live September tables. The `de.` subdomain serves the identical live table (English hero names, same month filter, refreshed within minutes). Direct scripted HTTP to any Dotabuff host is Cloudflare-blocked here, so raw reader dumps land in `matchups/raw/` and `scripts/fetch-matchups.ps1` (run via `make fetch-matchups`) validates freshness markers, row counts, and pool-pair mirror consistency before writing any file.

## archive contents and one loss

`matchups/archive-dotabuff/raw-www-2024-25/` holds the 8 verbatim stale English snapshots (arc-warden, doom, kunkka, visage, hoodwink, ogre-magi, winter-wyvern, warlock, stamps 2024-07 to 2025-06), re-fetched 2026-09-24 after an archive-collision bug (Move-Item -Force overwriting same-named files across same-day runs) destroyed the derived md copies. The bug is fixed, collisions now get timestamp suffixes. The August 2026 English tables for the other 9 heroes were lost to the same collision and are not retained: they were superseded by the live September window, and the English pages serve a fresh window anyway if a copy is ever needed.

## json snapshots

- `current-<date>.json`: machine readable copy of all 18 previous-pool rebuilt tables plus the mirror check result.
- `opendota-<date>.json`: OpenDota `heroStats` overall winrates (recent public window) for context lines. The `/api/heroes/{id}/matchups` endpoint is deliberately unused and should stay unused: its rolling sample is tiny (median 9 games per pair for Visage, 8 for Arc Warden) and it sign-flipped against fresh Dotabuff mirrors on both 300+ game pairs checked (Hoodwink vs Tiny, Windranger vs Tiny). It is not a Dotabuff substitute.

## index

All 18 files `matchups/<slug>.md`: dotabuff live 2026-09-24, de locale, one shared window, frozen reference since the scoring retirement (2026-09-29). Freshness and mirror stats per file are in each file header and in `current-*.json`.
