import json
import os

# regenerate the curation fragments from the committed mechanics + curated
# provenance so the fragments are the current source of truth. run after any
# deliberate content/curated mechanics change; the merge refuses fragments
# that disagree with committed text without fresh verdicts.
content = json.load(open("content.json", encoding="utf-8"))
curated = json.load(open("ref/dota2/howdoiplay/curated.json", encoding="utf-8"))

os.makedirs("var/howdoiplay-curation", exist_ok=True)
n = 0
for slug, hero in content["heroes"].items():
    # heroes not yet in curated.json are new pool additions: their fragments
    # are hand-authored and merged (which creates the curated entries)
    if slug not in curated["heroes"]:
        continue
    refs = curated["heroes"][slug]
    mech = hero["mechanics"]
    assert len(refs) == len(mech), (slug, len(refs), len(mech))
    lines = []
    for i, (tag, text) in enumerate(mech):
        assert refs[i]["idx"] == i
        entry = {"tag": tag, "text": text, "from": refs[i]["from"]}
        if "verified" in refs[i]:
            entry["verified"] = dict(refs[i]["verified"])
            # stamp the covered text so the merge can tell a carried verdict
            # from one that actually verified this wording
            entry["verified"]["text"] = text
        lines.append(entry)
    frag = {"slug": slug, "lines": lines}
    with open(f"var/howdoiplay-curation/{slug}.json", "w", encoding="utf-8", newline="\n") as f:
        json.dump(frag, f, indent=2)
        f.write("\n")
    n += 1
print(f"regenerated {n} fragments")
