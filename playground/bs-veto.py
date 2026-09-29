import json, glob, os

root = os.path.join(os.path.dirname(__file__), "..", "ref", "dota2", "synergy", "raw")
id2slug = {}
for p in glob.glob(os.path.join(root, "*.json")):
    with open(p, encoding="utf-8") as f:
        d = json.load(f)
    if isinstance(d, dict) and "heroId" in d and "slug" in d:
        id2slug[d["heroId"]] = d["slug"]

import sys

slug = sys.argv[1] if len(sys.argv) > 1 else "bloodseeker"
floor = int(sys.argv[2]) if len(sys.argv) > 2 else 100
side = sys.argv[3] if len(sys.argv) > 3 else "vs"
with open(os.path.join(root, f"{slug}.json"), encoding="utf-8") as f:
    bs = json.load(f)

raw = bs[side]
rows = []
for r in raw:
    m, w = r["matchCount"], r["winCount"]
    if m >= floor:
        rows.append((w / m, m, id2slug.get(r["heroId2"], f'id{r["heroId2"]}'), r.get("synergy", 0.0)))
# vs rows order by matchup winrate, with rows by the stratz expectation-adjusted
# synergy pp delta, so worst/best read as worst/best duos on the ally side
if side == "with":
    rows.sort(key=lambda x: x[3])
else:
    rows.sort()
show_syn = side == "with"

total_m = sum(r["matchCount"] for r in raw)
total_w = sum(r["winCount"] for r in raw)
print(f"overall: {total_w}/{total_m} = {total_w/total_m:.3f}")
print(f"\nworst (matchCount >= {floor}):")
for wr, m, s, syn in rows[:12]:
    print(f"  {s:22s} {wr:.3f} on {m}" + (f" syn {syn:+.1f}" if show_syn else ""))
print(f"\nbest (matchCount >= {floor}):")
for wr, m, s, syn in rows[-12:]:
    print(f"  {s:22s} {wr:.3f} on {m}" + (f" syn {syn:+.1f}" if show_syn else ""))
