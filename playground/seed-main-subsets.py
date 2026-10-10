import json
import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "scripts"))

import subsets

NAME = "main"
POOL = {
    "1": ["natures-prophet", "slark", "lone-druid", "lifestealer", "dragon-knight", "anti-mage"],
    "2": ["dragon-knight", "slark", "pangolier", "necrophos"],
    "3": ["necrophos", "dragon-knight", "axe", "pangolier"],
    "4": ["mirana", "hoodwink", "windranger"],
    "5": ["mirana", "hoodwink", "windranger"],
}


def assert_pool_seats():
    with open(os.path.join(ROOT, "config.json"), encoding="utf8") as fh:
        seats = {(e["slug"], e["role"]) for e in json.load(fh)["pool"]}
    for role, slugs in POOL.items():
        for slug in slugs:
            assert (slug, role) in seats, f"{slug}@{role} has no config pool seat, the picker would drop it"


def sorted_entries():
    pairs = [(slug, role) for role, slugs in POOL.items() for slug in slugs]
    return [{"slug": slug, "role": role} for slug, role in sorted(pairs)]


def reset_sequence():
    con = subsets.connect()
    try:
        exists = con.execute(
            "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'"
        ).fetchone()
        if exists:
            con.execute("DELETE FROM sqlite_sequence WHERE name = 'subsets'")
            con.commit()
    finally:
        con.close()


def seeded_already(want):
    subs = subsets.list_subsets()
    return len(subs) == 1 and subs[0]["name"] == NAME and subs[0]["entries"] == want


def seed():
    assert_pool_seats()
    subsets.init_db()
    want = sorted_entries()
    if not seeded_already(want):
        for s in subsets.list_subsets():
            if s["name"] == NAME:
                subsets.delete_subset(s["id"])
        reset_sequence()
        sid, _ = subsets.create_subset(NAME)
        subsets.update_subset(sid, entries=want)
    got = [s for s in subsets.list_subsets() if s["name"] == NAME]
    assert len(got) == 1 and got[0]["entries"] == want and len(want) == 20, got
    print(f"subset {NAME} holds {len(want)} sorted entries in {subsets.DB_PATH}")


if __name__ == "__main__":
    seed()
