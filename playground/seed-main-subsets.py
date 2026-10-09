import os
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(ROOT, "scripts"))

import subsets

NAME = "main"
POOL = {
    "1": ["slark", "lifestealer", "lone-druid", "natures-prophet", "necrophos"],
    "2": ["slark", "dragon-knight", "necrophos"],
    "3": ["necrophos", "enigma", "tidehunter", "slark"],
    "4": ["mirana", "hoodwink", "windranger", "ogre-magi"],
    "5": ["mirana", "hoodwink", "windranger", "ogre-magi"],
}


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
