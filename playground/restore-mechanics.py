import json
import subprocess
import sys

# restore the wiki-verified mechanics lines for the pre-existing heroes that
# the stale-fragment re-merge clobbered in 476ebbd, keeping bloodseeker's new
# block. curated provenance rides along from the same commit.
OLD_REF = "476ebbd^"

old_content = json.loads(
    subprocess.run(["git", "show", f"{OLD_REF}:content.json"], capture_output=True, text=True, check=True).stdout
)
old_curated = json.loads(
    subprocess.run(["git", "show", f"{OLD_REF}:ref/dota2/howdoiplay/curated.json"], capture_output=True, text=True, check=True).stdout
)

cur_path = "content.json"
lines = open(cur_path, encoding="utf-8").read().split("\n")
cur_slugs = json.loads("\n".join(lines))["heroes"].keys()
restored = []
hero = None
for i, line in enumerate(lines):
    if line.startswith('    "'):
        hero = line.split('"')[1]
    if line.startswith('      "mechanics":') and hero in old_content["heroes"]:
        assert hero in cur_slugs
        mech = old_content["heroes"][hero]["mechanics"]
        repl = '      "mechanics": ' + json.dumps(mech, ensure_ascii=False, separators=(", ", ": "))
        assert json.loads(repl.strip().split(": ", 1)[1]) == mech
        lines[i] = repl
        restored.append(hero)
open(cur_path, "w", encoding="utf-8", newline="\n").write("\n".join(lines))

curated = json.load(open("ref/dota2/howdoiplay/curated.json", encoding="utf-8"))
kept = 0
for slug in old_curated["heroes"]:
    assert slug in curated["heroes"] or slug in cur_slugs, slug
    if slug in curated["heroes"]:
        kept += 1
    curated["heroes"][slug] = old_curated["heroes"][slug]
json.dump(curated, open("ref/dota2/howdoiplay/curated.json", "w", encoding="utf-8", newline="\n"), indent=2)
open("ref/dota2/howdoiplay/curated.json", "a", encoding="utf-8", newline="\n").write("\n")

assert set(restored) == set(old_content["heroes"]) - {"bloodseeker"}, sorted(set(restored) ^ (set(old_content["heroes"]) - {"bloodseeker"}))
assert "bloodseeker" in curated["heroes"]
nv = sum(1 for h in curated["heroes"].values() for e in h if "verified" in e)
tot = sum(len(h) for h in curated["heroes"].values())
print(f"restored mechanics for {len(restored)} heroes, curated {len(curated['heroes'])} heroes, {nv}/{tot} entries verified")
sys.exit(0 if nv == tot else 1)
