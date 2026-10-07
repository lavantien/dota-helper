# sqlite storage for user-defined hero sub-pools, served over the loopback
# api in scripts/serve.py. delete-journal mode plus foreign keys on every
# connection, so no -wal/-shm sidecar can ever sit beside the db file.
import argparse
import os
import sqlite3

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DB_PATH = os.environ.get("SUBSETS_DB", os.path.join(ROOT, "var", "subsets.db"))

SCHEMA = """
CREATE TABLE IF NOT EXISTS subsets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE
);
CREATE TABLE IF NOT EXISTS subset_entries (
  subset_id INTEGER NOT NULL REFERENCES subsets(id) ON DELETE CASCADE,
  slug TEXT NOT NULL,
  role TEXT NOT NULL,
  PRIMARY KEY (subset_id, slug, role)
);
"""


class DuplicateNameError(Exception):
    pass


class MissingSubsetError(Exception):
    pass


def connect():
    con = sqlite3.connect(DB_PATH)
    con.execute("PRAGMA journal_mode=DELETE")
    con.execute("PRAGMA foreign_keys=ON")
    return con


def init_db():
    parent = os.path.dirname(DB_PATH)
    if parent:
        os.makedirs(parent, exist_ok=True)
    con = connect()
    try:
        exists = con.execute(
            "SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'subsets'"
        ).fetchone()
        if exists:
            # an already-initialized db is committed to git, a plain boot
            # must not rewrite its bytes
            return
        con.executescript(SCHEMA)
        con.commit()
        con.execute("VACUUM")
    finally:
        con.close()


def list_subsets():
    con = connect()
    try:
        subs = con.execute("SELECT id, name FROM subsets ORDER BY id").fetchall()
        rows = con.execute(
            "SELECT subset_id, slug, role FROM subset_entries"
            " ORDER BY subset_id, slug, role"
        ).fetchall()
    finally:
        con.close()
    out = {sid: {"id": sid, "name": name, "entries": []} for sid, name in subs}
    for sid, slug, role in rows:
        out[sid]["entries"].append({"slug": slug, "role": role})
    return [out[sid] for sid, _ in subs]


def get_subset(sid):
    con = connect()
    try:
        row = con.execute("SELECT name FROM subsets WHERE id = ?", (sid,)).fetchone()
        if row is None:
            raise MissingSubsetError(sid)
        entries = [
            {"slug": slug, "role": role}
            for slug, role in con.execute(
                "SELECT slug, role FROM subset_entries"
                " WHERE subset_id = ? ORDER BY slug, role",
                (sid,),
            )
        ]
    finally:
        con.close()
    return {"id": sid, "name": row[0], "entries": entries}


def create_subset(name):
    con = connect()
    try:
        cur = con.execute("INSERT INTO subsets (name) VALUES (?)", (name,))
        con.commit()
        return cur.lastrowid, name
    except sqlite3.IntegrityError as exc:
        con.rollback()
        raise DuplicateNameError(name) from exc
    finally:
        con.close()


def update_subset(sid, name=None, entries=None):
    # entries is a full replace inside one transaction, None leaves them alone
    con = connect()
    try:
        if con.execute("SELECT 1 FROM subsets WHERE id = ?", (sid,)).fetchone() is None:
            raise MissingSubsetError(sid)
        if name is not None:
            con.execute("UPDATE subsets SET name = ? WHERE id = ?", (name, sid))
        if entries is not None:
            con.execute("DELETE FROM subset_entries WHERE subset_id = ?", (sid,))
            con.executemany(
                "INSERT INTO subset_entries (subset_id, slug, role) VALUES (?, ?, ?)",
                [(sid, e["slug"], e["role"]) for e in entries],
            )
        con.commit()
    except sqlite3.IntegrityError as exc:
        con.rollback()
        raise DuplicateNameError(name) from exc
    except Exception:
        con.rollback()
        raise
    finally:
        con.close()


def delete_subset(sid):
    con = connect()
    try:
        cur = con.execute("DELETE FROM subsets WHERE id = ?", (sid,))
        con.commit()
        if cur.rowcount == 0:
            raise MissingSubsetError(sid)
    finally:
        con.close()


def main():
    parser = argparse.ArgumentParser(description="hero sub-pool storage")
    parser.add_argument("command", choices=("init",))
    parser.parse_args()
    init_db()


if __name__ == "__main__":
    main()
