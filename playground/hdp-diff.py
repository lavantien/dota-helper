import json, sys

old = json.load(open("var/howdoiplay-before.json", encoding="utf-8"))["heroes"]
new = json.load(open("ref/dota2/howdoiplay/howdoiplay.json", encoding="utf-8"))["heroes"]

bad = 0
for slug in sorted(old):
    if slug not in new:
        # a hero dropped from the pool is not re-crawled; its fragment and
        # curated entry leave with it, so there is nothing to remap
        print(f"DROPPED {slug}: not in the new snapshot, skipped")
        continue
    for sec in ("tips", "counters"):
        a = [t["text"] for t in old[slug][sec]]
        b = [t["text"] for t in new[slug][sec]]
        if a == b:
            continue
        if b[: len(a)] == a:
            print(f"APPENDONLY {slug} {sec}: {len(a)} -> {len(b)}")
            continue
        bad += 1
        # find the first divergence to judge remap need
        i = next((i for i in range(min(len(a), len(b))) if a[i] != b[i]), min(len(a), len(b)))
        print(f"CHANGED {slug} {sec}: {len(a)} -> {len(b)}, first diff at {i}")
        print(f"  old[{i}]: {a[i][:100] if i < len(a) else '<none>'}")
        print(f"  new[{i}]: {b[i][:100] if i < len(b) else '<none>'}")

print("remap needed" if bad else "no remap needed")
sys.exit(1 if bad else 0)
