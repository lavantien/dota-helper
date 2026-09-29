# Reads the merged engine coverage profile, enforces the project floor, and
# writes the shields.io endpoint JSON the readme badge renders. Runs via
# `make coverage-badge` after `make engine-cover`.
import json
import sys
from pathlib import Path

FLOOR = 90.0
OUT = Path("coverage.json")


def main() -> int:
    profile = Path("engine/coverage.out")
    if not profile.exists():
        print(f"no profile at {profile}, run make engine-cover first")
        return 1

    # merged atomic profiles repeat each block once per test binary, so sum
    # counts per location and weight by statement count, the same math
    # `go tool cover -func` reports as its total
    stmts_at = {}
    for line in profile.read_text(encoding="utf-8").splitlines():
        if line.startswith("mode:"):
            continue
        loc, stmts, count = line.rsplit(" ", 2)
        hit, _ = stmts_at.get(loc, (0, 0))
        # the statement count is identical on every duplicate of a block, so
        # overwrite it while the execution counts keep summing
        stmts_at[loc] = (hit + int(count), int(stmts))
    total = sum(t for _, t in stmts_at.values())
    covered = sum(t for c, t in stmts_at.values() if c > 0)
    if not total:
        print("profile has no statements")
        return 1
    pct = 100.0 * covered / total

    color = "brightgreen" if pct >= FLOOR else ("orange" if pct >= 80 else "red")
    badge = {
        "schemaVersion": 1,
        "label": "coverage",
        "message": f"{pct:.1f}%",
        "color": color,
        "cacheSeconds": 3600,
    }
    OUT.write_text(json.dumps(badge) + "\n", encoding="utf-8")
    print(f"coverage {pct:.1f}% of {total} statements, badge -> {OUT}")
    if pct < FLOOR:
        print(f"below the {FLOOR:.0f}% floor")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
