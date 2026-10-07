.PHONY: dota picker guide engine-build engine-test engine-cover engine-cover-summary test-picker test-picker-cover test probe-stratz probe-opendota \
        probe-builds probe-matches probe-scope probe-window fetch-stratz fetch-stratz-refresh fetch-opendota fetch-builds \
        fetch-builds-refresh fetch-matches fetch-matches-refresh fetch-positions ingest db-query mine sync-builds order-pool emit emit-data \
        emit-guide emit-picker emit-goldens eval eval-fit eval-fit-alphas eval-fit-completion \
        eval-promote refresh fixtures verify fetch-matchups \
        build-guide-data fetch-howdoiplay merge-mechanics validate-curation check check-linux coverage-badge shots

# duckdb cgo has no supported native windows toolchain (crt-mixing, verified),
# so every go build and test runs in the linux golang image, same pattern as the
# capstone analytics module. named volumes keep module and build caches warm.
ENGINE_DIR = engine
BIN = var/engine
GO_IMG = golang:1.27
PORT ?= 8631
GO_RUN = docker run --rm -v $(CURDIR):/src -w /src -v poolguide-mod:/go/pkg/mod -v poolguide-build:/root/.cache/go-build -e CGO_ENABLED=1 -e STRATZ_TOKEN $(GO_IMG)

# git-for-windows make runs multi-command recipes through msys sh, whose
# runtime rewrites container paths like -w /src into host git paths before
# docker.exe sees them; disable that rewrite (no-op on the linux ci host)
export MSYS_NO_PATHCONV := 1

# dota stays the habitual one-key target, it opens the entry point now
dota:
	pwsh -NoProfile -Command "Start-Process (Join-Path (Get-Location) 'picker/picker.html')"

picker:
	pwsh -NoProfile -Command "Start-Process (Join-Path (Get-Location) 'picker/picker.html')"

guide:
	pwsh -NoProfile -Command "Start-Process (Join-Path (Get-Location) 'guide/index.html')"

# static server for visual checks over http (file:// is blocked in browser
# tooling); loopback only, page trees only, so var/ and .git stay unserved
serve:
	python scripts/serve.py $(PORT)

# readme art: headless capture of both pages through their shot bootstraps.
# pre release step, so the screenshots always match the shipped pages
shots:
	pwsh -NoProfile -File scripts/take-shots.ps1 $(PORT)

engine-build:
	$(GO_RUN) go -C $(ENGINE_DIR) build -o ../$(BIN) ./cmd/engine

# -count=1: the emit golden tests read content.json at runtime, which sits
# outside go's test cache key, so a warm volume would mask prose drift.
# -timeout: ./... runs every package binary concurrently, and the two slow
# ones (emit over the pool fixtures, eval over the fit pipeline) each take
# ~20m solo and stretch past 30m when they share cores, so the per-binary
# cap sits at twice that
engine-test:
	$(GO_RUN) go -C $(ENGINE_DIR) test -count=1 -timeout 60m ./...

# scoped loop for one internal package during TDD: make engine-test-stats
# (-timeout for the same cold-cache reason as engine-test: a cgo build plus a
# concurrent container can exceed go's 600s per-package default)
engine-test-%:
	$(GO_RUN) go -C $(ENGINE_DIR) test -count=1 -timeout 30m ./internal/$*/...

# coverage twin of engine-test. -coverpkg scopes measurement to internal and
# makes cross-package exercise count (emit tests driving mine, eval driving
# emit): cmd/engine is process-level wiring over live crawls and carries no
# test binary. -covermode=atomic because ./... runs package binaries
# concurrently; -timeout sits above engine-test's since instrumentation adds
# overhead on top of the two ~20m binaries. the profile lands in engine/ on
# the host through the -v $(CURDIR):/src mount.
engine-cover:
	$(GO_RUN) go -C $(ENGINE_DIR) test -count=1 -timeout 90m -covermode=atomic -coverprofile=coverage.out -coverpkg=./internal/... ./internal/...
	$(GO_RUN) go -C $(ENGINE_DIR) tool cover -func=coverage.out

# statement-weighted rollup of the last engine-cover profile: per-package
# and per-function table sorted worst-first, the gap-closure work queue
engine-cover-summary:
	python scripts/cover-summary.py $(ENGINE_DIR)/coverage.out

# fails below the project floor and writes the shields endpoint json the
# readme badge renders (ci commits the file after a coverage run)
coverage-badge:
	python scripts/coverage-badge.py

# scoped loop for one internal package during coverage gap closure, same
# shape as engine-test-%: measures the package's binary against the whole
# internal surface, so the number is a lower bound on the merged profile
engine-cover-%:
	$(GO_RUN) go -C $(ENGINE_DIR) test -count=1 -timeout 30m -covermode=atomic -coverprofile=coverage-$*.out -coverpkg=./internal/... ./internal/$*/...
	python scripts/cover-summary.py $(ENGINE_DIR)/coverage-$*.out

test-picker:
	node --test picker/picker-score.test.mjs

# lcov twin of test-picker feeding codecov alongside the engine profile; the
# spec reporter keeps the local loop readable, only picker-score.js loads so
# the report covers exactly the scored core, and the test file is excluded
test-picker-cover:
	node --test --experimental-test-coverage \
	     --test-coverage-exclude=**/*.test.mjs \
	     --test-reporter=spec --test-reporter-destination=stdout \
	     --test-reporter=lcov --test-reporter-destination=picker-lcov.info \
	     picker/picker-score.test.mjs

test: engine-test test-picker

probe-stratz: engine-build
	$(GO_RUN) ./$(BIN) probe stratz

probe-opendota: engine-build
	$(GO_RUN) ./$(BIN) probe opendota

probe-builds: engine-build
	$(GO_RUN) ./$(BIN) probe builds

# live shape/coverage probe for the per-match draft backfill
probe-matches: engine-build
	$(GO_RUN) ./$(BIN) probe matches

# live accept/reject ladder for gameMode/lobbyType/position filter args on
# the heroStats aggregates (scope.gameMode/lobbyType scoping evidence)
probe-scope: engine-build
	$(GO_RUN) ./$(BIN) probe scope

# live window-honoring ladder for the heroStats families against the hub take:
# hard-fails when a take-controllable endpoint refuses the pinned window
probe-window: engine-build
	$(GO_RUN) ./$(BIN) probe window

fetch-stratz: engine-build
	$(GO_RUN) ./$(BIN) fetch stratz

# force-refetches every hero cache instead of resuming around fresh ones
fetch-stratz-refresh: engine-build
	$(GO_RUN) ./$(BIN) fetch stratz -refresh

fetch-opendota: engine-build
	$(GO_RUN) ./$(BIN) fetch opendota

# stratz item-build crawl feeding the content.json build/timings sync
fetch-builds: engine-build
	$(GO_RUN) ./$(BIN) fetch builds

fetch-builds-refresh: engine-build
	$(GO_RUN) ./$(BIN) fetch builds -refresh

# per-match draft backfill from stratz league matches into ref/dota2/matches/raw
fetch-matches: engine-build
	$(GO_RUN) ./$(BIN) fetch matches

# re-opens a done backfill manifest: window end extends to now, the leagues
# cache re-lists, dedupe keeps committed matches unique
fetch-matches-refresh: engine-build
	$(GO_RUN) ./$(BIN) fetch matches -refresh

# per-farm-position winDay aggregates (5 requests, display data only)
fetch-positions: engine-build
	$(GO_RUN) ./$(BIN) fetch positions

ingest: engine-build
	$(GO_RUN) ./$(BIN) ingest

# read-only sql against the live duckdb, e.g. make db-query Q="'select count(*) from match_raw'"
db-query: engine-build
	$(GO_RUN) ./$(BIN) db-query $(Q)

mine: engine-build
	$(GO_RUN) ./$(BIN) mine

# derives hero builds from the committed builds crawl and syncs content.json
sync-builds: engine-build
	$(GO_RUN) ./$(BIN) sync builds

# re-sorts each role's pool entries and the gates fallbackOrder by mined
# per-position win rate (divine-immortal ranked all pick, display order only,
# membership never touched)
order-pool: engine-build
	$(GO_RUN) ./$(BIN) sync order

emit: engine-build
	$(GO_RUN) ./$(BIN) emit

# single-step emits: bare emit regenerates every artifact (and rewrites each
# date line against the live db), these regenerate exactly one
emit-data: engine-build
	$(GO_RUN) ./$(BIN) emit data

emit-guide: engine-build
	$(GO_RUN) ./$(BIN) emit guide

emit-picker: engine-build
	$(GO_RUN) ./$(BIN) emit picker

# rewrites the emit golden pins after an intentional config hub change
# (-timeout: the suite re-emits every artifact at testdata scale, which crossed
# the go default 10m once the pool grew past 64 entries)
emit-goldens:
	$(GO_RUN) go -C $(ENGINE_DIR) test -timeout 30m ./internal/emit -update

# order-aware eval: replays the committed league drafts against the live
# picker model and writes ref/dota2/eval/latest.json (see ref/dota2/eval/README.md)
eval: engine-build
	$(GO_RUN) ./$(BIN) eval report

# phase 4 fits, proposals land in var/ (gitignored) beside paths.fitOut:
# weights by coordinate ascent on train mean pick percentile, shrinkage
# alphas by empirical-Bayes moment matching, completion rank/lambda by
# masked-cell CV
eval-fit: engine-build
	$(GO_RUN) ./$(BIN) eval fit weights

eval-fit-alphas: engine-build
	$(GO_RUN) ./$(BIN) eval fit alphas

eval-fit-completion: engine-build
	$(GO_RUN) ./$(BIN) eval fit completion

# applies one fit proposal behind its guard (SECTION=weights|alphas|completion)
# and rebuilds the chain it feeds: alphas and completion flow through the
# mined tables, weights through the emitted picker data
SECTION ?= weights
eval-promote: engine-build
	$(GO_RUN) ./$(BIN) eval promote $(SECTION)
	$(MAKE) --no-print-directory mine emit emit-goldens test

# unified data refresh, one command for the whole pipeline: stratz is the
# single live source (matchups, synergy, popularity, overall win rates, item
# builds, positions, trends), so refresh force-refetches every hero cache,
# the builds crawl, the positions crawl, and the per-match backfill (re-opened
# window, match-id dedupe), re-syncs the stratz-derived build/timings fields
# into content.json, rebuilds all derived data and both artifacts, re-sorts
# the pool order and fallbackOrder from the fresh per-position win rates
# (order-pool rewrites config.json and gates.json the same way sync-builds
# rewrites content.json, so ordering churn on a refresh is intentional, not
# drift), re-pins the
# emit goldens (sync-builds rewrites content.json prose the goldens embed, so
# a refresh is an intentional golden change, not drift), appends the day's
# trends snapshot, then regenerates the eval report and verifies the whole
# chain end to end. fits and promotions stay manual and guarded
# (make eval-fit*, eval-promote SECTION=...). serial only: fetch-builds needs
# the roster cache fetch-stratz writes, so never run this with -j. needs the
# stratz token at var/stratz.token (or STRATZ_TOKEN in env before docker).
# opendota is manual fallback tooling only: run make fetch-opendota when the
# crawl is unusable.
refresh: fetch-stratz-refresh fetch-builds-refresh fetch-positions fetch-matches-refresh sync-builds
	$(MAKE) --no-print-directory ingest mine order-pool emit emit-goldens test check eval

fixtures: engine-build
	$(GO_RUN) ./$(BIN) fixtures

verify: engine-build
	$(GO_RUN) ./$(BIN) verify

fetch-matchups:
	pwsh -NoProfile -File scripts/fetch-matchups.ps1

# patch-scoped prose crawl, rerun when the patch series moves (not in refresh)
fetch-howdoiplay:
	pwsh -NoProfile -File scripts/fetch-howdoiplay.ps1

build-guide-data:
	pwsh -NoProfile -File scripts/build-guide-data.ps1

# validate var/howdoiplay-curation fragments against the fresh howdoiplay extraction
validate-curation:
	node playground/validate-curation.mjs

# fold var/howdoiplay-curation fragments into content.json mechanics + curated.json
merge-mechanics:
	node playground/merge-mechanics.mjs

check:
	pwsh -NoProfile -File scripts/check.ps1

# ci parity: the check job runs scripts/check.ps1 under linux pwsh, where
# path handling diverges from the windows host
check-linux:
	docker run --rm -v $(CURDIR):/src -w /src --entrypoint bash mcr.microsoft.com/powershell:latest \
	  -c "apt-get update -qq >/dev/null && apt-get install -y -qq git >/dev/null 2>&1 && pwsh -NoProfile -File scripts/check.ps1"
