import json, os, sys

root = os.path.join(os.path.dirname(__file__), "..", "ref", "dota2", "builds", "raw")
cfg = json.load(open(os.path.join(os.path.dirname(__file__), "..", "config.json"), encoding="utf-8"))
builds_cfg = cfg["builds"]
aliases = builds_cfg["itemAliases"]
content = json.load(open(os.path.join(os.path.dirname(__file__), "..", "content.json"), encoding="utf-8"))["heroes"]

items = json.load(open(os.path.join(root, "_items.json"), encoding="utf-8"))["items"]
by_id = {it["id"]: it for it in items}
parents = {}
for it in items:
    if not it["shortName"].startswith("recipe_"):
        continue
    result = it["shortName"][len("recipe_"):]
    for c in (it.get("components") or []):
        parents.setdefault(c["componentId"], set()).add(result)

def token(short):
    return aliases.get(short, short.replace("_", " "))

def derive_entry(rows):
    agg = {}
    for r in rows:
        if r["instance"] != 0:
            continue
        a = agg.get(r["itemId"], [0, 0])
        a[0] += r["matchCount"]
        a[1] += r["time"] * r["matchCount"]
        agg[r["itemId"]] = a
    cands = []
    for iid, (m, mom) in agg.items():
        it = by_id.get(iid)
        if it is None or it["stat"]["isRecipe"] or it["shortName"].startswith("recipe_"):
            continue
        if it["stat"]["cost"] < builds_cfg["minCost"] or m < builds_cfg["minMatches"]:
            continue
        cands.append({"id": iid, "name": it["shortName"], "m": m, "avg": mom / m if m else 0.0})
    cands.sort(key=lambda c: (-c["m"], c["id"]))
    shorts = {c["name"] for c in cands}
    kept = [c for c in cands if not (parents.get(c["id"], set()) & shorts)]
    kept = kept[: builds_cfg["topN"]]
    total = kept[0]["m"]
    kept.sort(key=lambda c: (c["avg"], -c["m"], c["id"]))
    toks = [token(c["name"]) for c in kept]
    timings = [(token(c["name"]), int(c["avg"] + 0.5)) for c in kept[: builds_cfg["timingTopN"]]]
    return toks, timings, total

def derive_hero(slug, roles):
    by_role = {}
    thin = []
    for r in roles:
        p = os.path.join(root, f"{slug}@{r}.json")
        if not os.path.exists(p):
            thin.append(r)
            continue
        rows = json.load(open(p, encoding="utf-8"))["rows"]
        try:
            by_role[r] = derive_entry(rows)
        except Exception:
            thin.append(r)
    roles_kept = sorted(by_role)
    if len(roles_kept) == 1:
        b = by_role[roles_kept[0]]
        return ", ".join(b[0]), b[1], roles_kept, thin
    segs = []
    for r in roles_kept:
        t = by_role[r][0]
        if segs and segs[-1][1] == t:
            segs[-1][0].append(r)
        else:
            segs.append(([r], t))
    s = ". ".join(f"pos {'/'.join(rl)}: {', '.join(tk)}" for rl, tk in segs)
    best = max(roles_kept, key=lambda r: by_role[r][2])
    return s, by_role[best][1], roles_kept, thin

pool_by_hero = {}
for h in cfg["pool"]:
    pool_by_hero.setdefault(h["slug"], []).append(h["role"])

target = sys.argv[1:] or ["bloodseeker", "windranger", "abaddon", "doom", "medusa", "mirana"]
bad = 0
for slug in target:
    roles = sorted(set(pool_by_hero.get(slug, [])))
    if not roles:
        print(f"{slug}: not in pool")
        continue
    build, timings, kept, thin = derive_hero(slug, roles)
    c = content[slug]
    ok_b = build == c["build"]
    ok_t = [list(t) for t in timings] == c["timings"]
    status = "OK" if (ok_b and ok_t) else "MISMATCH"
    if not (ok_b and ok_t):
        bad += 1
    print(f"{slug} roles={roles} kept={kept} thin={thin} -> {status}")
    if not ok_b:
        print(f"  derived build : {build}")
        print(f"  content build : {c['build']}")
    if not ok_t:
        print(f"  derived timing: {timings}")
        print(f"  content timing: {c['timings']}")
print("mismatches:", bad)
