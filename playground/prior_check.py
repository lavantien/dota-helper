# read-only probe: dump hero_prior and recompute the emitted prior transform
import duckdb, json, re, sys

con = duckdb.connect("var/dota.duckdb", read_only=True)
rows = con.execute("SELECT slug, wr, tier, value FROM hero_prior ORDER BY slug").fetchall()
tiers = {r[0]: r[2] for r in rows}
wrs = {r[0]: r[1] for r in rows}
vals = {r[0]: r[3] for r in rows}

gen = json.load(open("playground/prior_gen.json", encoding="utf8"))
prior, pool_idx, slugs = gen["prior"], gen["poolIdx"], gen["slugs"]

cfg = json.load(open("config.json", encoding="utf8"))
pool = sorted({e["slug"] for e in cfg["pool"]})
print("pool order == slug ascending:", pool == sorted(pool), "n =", len(pool))
print("emitted poolIdx maps to slugs:", [slugs[i] for i in pool_idx][:5], "...")

# z of wrs in pool order (pure prior per internal/analytics/prior.go)
mu = sum(wrs[s] for s in pool) / len(pool)
var = sum((wrs[s] - mu) ** 2 for s in pool) / len(pool)
import math
sd = math.sqrt(var)
z = {s: (wrs[s] - mu) / sd for s in pool}

# midrank percentiles, ties averaged, (avgRank - 0.5) / n  (analytics.Midrank)
def midrank(d):
    items = sorted(d, key=lambda s: d[s])
    out, i, n = {}, 0, len(items)
    while i < n:
        j = i
        while j + 1 < n and d[items[j + 1]] == d[items[i]]:
            j += 1
        avg = (i + j + 2) / 2
        for k in range(i, j + 1):
            out[items[k]] = (avg - 0.5) / n
        i = j + 1
    return out

pct = midrank(z)
pct_wr = midrank(wrs)  # must be identical: z is monotone affine in wr

bad = 0
for i, s in enumerate(pool):
    e = prior[i]
    ok = abs(e - pct[s]) < 1e-3
    ok_wr = pct[s] == pct_wr[s]
    if not ok or not ok_wr:
        bad += 1
    print(f"{s:20s} wr={wrs[s]:.4f} tier={tiers[s]:9s} z={z[s]:+.3f} midrank={pct[s]:.4f} emitted={e:.4f} "
          f"zmatch={ok} wrorder={ok_wr}")

print("mismatches:", bad, "of", len(pool))
# tier correlation sanity: mean emitted prior by tier label
for t in sorted(set(tiers.values())):
    v = [prior[i] for i, s in enumerate(pool) if tiers[s] == t]
    print(f"tier {t}: n={len(v)} mean emitted prior={sum(v)/len(v):.4f}")
# stored value column should equal z (pure), not a blend
vbad = [s for s in pool if abs(vals[s] - z[s]) > 1e-6]
print("hero_prior.value == pure z mismatches:", vbad)
