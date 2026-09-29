# dota helper

[![ci](https://github.com/lavantien/dota-helper/actions/workflows/ci.yml/badge.svg)](https://github.com/lavantien/dota-helper/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/lavantien/dota-helper/badge.svg)](https://codecov.io/gh/lavantien/dota-helper)

offline dota 2 pick simulator and hero-pool guide for patch 7.41f. static pages with zero runtime deps, an offline go engine over duckdb, and one authored config hub. the picker is the main tool, the guide supplements it.

![the picker ranking pos 1 heroes mid draft, clinkz vetoed against the axe and earthshaker lockdown](docs/picker.png)

![the guide hero panel for clinkz, build, timings, tech, mechanics, and matchup tables](docs/guide.png)

## tools

- `picker/`: draft board with direct slot aiming, ban strip, and per-term scored ranking for the selected role
- `guide/`: patch state, hero panels, heatmap, trends, principles, practice
- `ui/ui.css`: shared dark-hud token sheet, single source for every color, type, spacing, and control-size value. both pages link it as `../ui/ui.css`, page css adds layout only

## layout

- `config.json` + `content.json`: the authored config hub (pool, roles, scope, scoring constants, paths) and authored prose, single source of truth
- `engine/`: go offline engine (fetch, ingest, mine, emit, eval) over duckdb, module `poolguide`
- `scripts/`: powershell orchestration, the matchups and howdoiplay crawls, the guide data build, and the check guard
- `ref/`: committed stratz crawl caches, matchup tables, and eval reports, the rebuildable raw layer
- `playground/`: one-off utilities
- `var/`: gitignored live state, the duckdb database, engine binary, eval fits, and the stratz token

## data flow

STRATZ graphql crawl (immortal, ranked all pick) into `ref/dota2/synergy/raw/` caches, per-match draft backfill (league captains-mode drafts with true pick/ban order) into `ref/dota2/matches/raw/matches.ndjson`, duckdb under `var/`, shrinkage + normalization + ALS completion + expectimax-lite scoring in the engine, emitted as generated js the static pages import.

## data policy

- STRATZ is always the primary source, the raw data the product uses is exclusively STRATZ. the API runs at 7 requests/second with the token at `var/stratz.token` (or `STRATZ_TOKEN` env), the token never goes into a tracked file, `make check` enforces it
- the dotabuff fallback tier retired from scoring on 2026-09-29, its dumps stay on disk locally as frozen reference and are not part of this repo, ingest actively purges their rows
- OpenDota is manual fallback tooling only, it never enters scoring
- per-match pub data is token-gated on the default STRATZ tier, so the draft backfill runs over league matches which carry full pick/ban drafts

## engine

the go engine runs only inside the golang:1.27 linux image (the duckdb cgo driver has no supported windows toolchain). every build, test, and invocation goes through make targets wrapping docker, never go on the host. named volumes `poolguide-mod` and `poolguide-build` keep the module and build caches warm.

## make targets

`make refresh` is the unified pipeline command (serial only, never -j). the others by group:

- view: `serve` (http server for browser tooling, file:// is blocked there), `picker`, `dota`
- fetch: `fetch-stratz`, `fetch-builds`, `fetch-matches`, `fetch-positions`, `fetch-howdoiplay`, `fetch-matchups`, plus `-refresh` variants
- build data: `ingest`, `mine`, `emit` (or `emit-data`, `emit-guide`, `emit-picker`), `emit-goldens`, `build-guide-data`, `sync-builds`, `order-pool`
- evaluate: `eval`, `eval-fit`, `eval-fit-alphas`, `eval-fit-completion`, `eval-promote SECTION=weights|alphas|completion`
- test: `test-picker`, `test-picker-cover`, `engine-test`, `engine-test-<pkg>`, `engine-cover`, `engine-cover-summary`, `engine-cover-<pkg>`, `check`, `check-linux`
- inspect: `db-query Q=...`, `probe-*`

## conventions

- generated files (`picker-data-generated.js`, `guide-data-generated.js`, `data.js`) are engine output, never edited by hand. after an intentional config change run `make ingest mine emit`, then `make emit-goldens`
- pool counts, roster counts, and hero lists are always derived from `config.json` or the roster table, never hardcoded
- hero prose is general first principles, no numerics in descriptions, matchup winrates live only in the generated tables, `make check` enforces the prose rules
- tier is a board label only, it never enters scoring, every hero scores from mined data under identical terms

## ci

two jobs on push to main: `check` (test-picker plus the static guard) and `coverage` (the dockerized engine suite with `make engine-cover` plus the node coverage run, uploaded to codecov, project gate above 90). the full suite takes 30-60m locally via `make engine-test`, `make check-linux` replays the static guard under linux pwsh the way ci runs it.

## license

mit
