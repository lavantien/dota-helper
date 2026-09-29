#!/usr/bin/env python3
"""statement-weighted coverage rollup from a merged go cover profile.

a merged profile (go test ./... -coverprofile) repeats each block once per
test binary that exercised it, so rows must be deduped by location with
counts summed before any math, otherwise duplicate zero rows dilute the
totals (the go tool cover -func total is the authority the rollup must
match). prints per-package and per-file covered/total statements worst
first plus uncovered block ranges per bad file, the gap-closure queue.
"""
import sys
from collections import defaultdict

blocks = {}  # loc -> [count_sum, num_stmts]
for path in sys.argv[1:]:
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("mode:"):
                continue
            loc, stmts, count = line.rsplit(" ", 2)
            stmts, count = int(stmts), int(count)
            b = blocks.setdefault(loc, [0, stmts])
            b[0] += count

if not blocks:
    sys.exit("no profile rows parsed")
pkg_stmts = defaultdict(lambda: [0, 0])
file_stmts = defaultdict(lambda: [0, 0])
file_ranges = defaultdict(list)
for loc, (count, stmts) in blocks.items():
    src = loc.rsplit(":", 1)[0].replace("\\", "/")
    parts = src.split("/")
    pkg = "/".join(parts[1:-1]) if len(parts) > 2 else parts[-2]
    covered = stmts if count > 0 else 0
    pkg_stmts[pkg][0] += covered
    pkg_stmts[pkg][1] += stmts
    file_stmts[src][0] += covered
    file_stmts[src][1] += stmts
    if count == 0:
        file_ranges[src].append(loc.rsplit(":", 1)[1].split(",")[0])

w = max(len(p) for p in pkg_stmts)
print(f"{'package':{w}}  {'cov':>7}  {'stmts':>6}")
for pkg, (cov, tot) in sorted(pkg_stmts.items(), key=lambda kv: kv[1][0] / max(kv[1][1], 1)):
    pct = 100.0 * cov / tot if tot else 0.0
    print(f"{pkg:{w}}  {pct:6.1f}%  {tot:6}")

gc = sum(v[0] for v in pkg_stmts.values())
gt = sum(v[1] for v in pkg_stmts.values())
print(f"\ntotal: {100.0 * gc / gt:.1f}% of {gt} statements\n")

print("files below half coverage, worst first:")
rows = [(c / t, c, t, src) for src, (c, t) in file_stmts.items() if t and c < t / 2]
for ratio, c, t, src in sorted(rows):
    ranges = ",".join(file_ranges[src][:8]) + ("..." if len(file_ranges[src]) > 8 else "")
    print(f"  {100.0 * c / t:5.1f}%  {c:5}/{t:<5} {src.split('internal/')[-1]}  uncovered@{ranges}")
print(f"{len(rows)} files below half coverage")
