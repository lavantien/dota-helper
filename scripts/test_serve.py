# e2e tests for scripts/serve.py: spawn the real server against a temp
# subsets db on a free port, then drive the static tree and the subsets api
# over http, covering the status contract and the sqlite side effects.
import json
import os
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.request

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SERVE = os.path.join(ROOT, "scripts", "serve.py")


def free_port():
    probe = socket.socket()
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
    probe.close()
    return port


def request(method, url, payload=None):
    # payload None sends no body, bytes go out raw so malformed json is testable
    data = None
    headers = {}
    if payload is not None:
        data = payload if isinstance(payload, bytes) else json.dumps(payload).encode()
        headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, resp.read()
    except urllib.error.HTTPError as e:
        body = e.read()
        e.close()
        return e.code, body


class ServerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.db = os.path.join(cls.tmp.name, "subsets.db")
        cls.port = free_port()
        cls.base = "http://127.0.0.1:%d" % cls.port
        cls.proc = subprocess.Popen(
            [sys.executable, SERVE, str(cls.port)],
            env=dict(os.environ, SUBSETS_DB=cls.db),
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        cls.addClassCleanup(cls.stop_server)
        deadline = time.monotonic() + 20
        last = "never answered"
        while time.monotonic() < deadline:
            try:
                status, _ = request("GET", cls.base + "/api/subsets")
                if status == 200:
                    break
                last = "status %d" % status
            except OSError as e:
                last = str(e)
            time.sleep(0.1)
        else:
            raise AssertionError("server never came up: %s" % last)

    @classmethod
    def stop_server(cls):
        cls.proc.terminate()
        try:
            cls.proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            cls.proc.kill()
            cls.proc.wait()
        cls.tmp.cleanup()

    def make_subset(self):
        # unique per test and per call, so shared-db tests never collide
        tag = "%s %d" % (self.id().rsplit(".", 1)[-1], time.monotonic_ns())
        status, body = request("POST", self.base + "/api/subsets", {"name": tag})
        self.assertEqual(status, 201, body)
        return json.loads(body)

    def test_db_created_on_boot(self):
        self.assertTrue(os.path.exists(self.db))

    def test_create_and_list(self):
        made = self.make_subset()
        status, body = request("GET", self.base + "/api/subsets")
        self.assertEqual(status, 200)
        listing = json.loads(body)["subsets"]
        self.assertIn(
            {"id": made["id"], "name": made["name"], "entries": []}, listing
        )

    def test_put_replaces_entries_and_keeps_them_on_rename(self):
        made = self.make_subset()
        url = "%s/api/subsets/%d" % (self.base, made["id"])
        status, body = request(
            "PUT",
            url,
            {"entries": [{"slug": "ursa", "role": "1"}, {"slug": "juggernaut", "role": "1"}]},
        )
        self.assertEqual(status, 200, body)
        updated = json.loads(body)
        # entries read back sorted by (slug, role), not payload order
        self.assertEqual(
            updated["entries"],
            [{"slug": "juggernaut", "role": "1"}, {"slug": "ursa", "role": "1"}],
        )
        status, body = request("PUT", url, {"name": made["name"] + " renamed"})
        self.assertEqual(status, 200, body)
        renamed = json.loads(body)
        self.assertEqual(renamed["name"], made["name"] + " renamed")
        self.assertEqual(renamed["entries"], updated["entries"])
        status, body = request("PUT", url, {"entries": []})
        self.assertEqual(status, 200, body)
        self.assertEqual(json.loads(body)["entries"], [])

    def test_put_bad_entries_rejected(self):
        made = self.make_subset()
        url = "%s/api/subsets/%d" % (self.base, made["id"])
        bad_payloads = (
            {"entries": "nope"},
            {"entries": [{"slug": "ursa"}]},
            {"entries": [{"slug": "ursa", "role": 1}]},
            {"entries": [{"slug": "ursa", "role": "1"}, {"slug": "ursa", "role": "1"}]},
        )
        for bad in bad_payloads:
            status, _ = request("PUT", url, bad)
            self.assertEqual(status, 400, bad)

    def test_delete_cascade(self):
        made = self.make_subset()
        request(
            "PUT",
            "%s/api/subsets/%d" % (self.base, made["id"]),
            {"entries": [{"slug": "ursa", "role": "1"}]},
        )
        status, body = request("DELETE", "%s/api/subsets/%d" % (self.base, made["id"]))
        self.assertEqual(status, 204)
        self.assertEqual(body, b"")
        con = sqlite3.connect(self.db)
        try:
            left = con.execute(
                "SELECT count(*) FROM subset_entries WHERE subset_id = ?", (made["id"],)
            ).fetchone()[0]
        finally:
            con.close()
        self.assertEqual(left, 0)

    def test_post_blank_name_rejected(self):
        for bad in ({}, {"name": "   "}, {"name": 3}, [1]):
            status, _ = request("POST", self.base + "/api/subsets", bad)
            self.assertEqual(status, 400, bad)

    def test_malformed_json_rejected(self):
        status, body = request("POST", self.base + "/api/subsets", b'{"name": ')
        self.assertEqual(status, 400)
        self.assertIn("error", json.loads(body))

    def test_oversized_body_rejected(self):
        status, _ = request(
            "POST", self.base + "/api/subsets", {"name": "x" * (1024 * 1024 + 10)}
        )
        self.assertEqual(status, 400)

    def test_unknown_id_rejected(self):
        status, _ = request("PUT", self.base + "/api/subsets/999999", {"name": "nope"})
        self.assertEqual(status, 404)
        status, _ = request("DELETE", self.base + "/api/subsets/999999")
        self.assertEqual(status, 404)

    def test_unknown_api_path_rejected(self):
        status, body = request("GET", self.base + "/api/nope")
        self.assertEqual(status, 404)
        self.assertIn("error", json.loads(body))

    def test_wrong_method_rejected(self):
        status, _ = request("DELETE", self.base + "/api/subsets")
        self.assertEqual(status, 405)
        status, _ = request("PUT", self.base + "/api/subsets", {})
        self.assertEqual(status, 405)
        status, _ = request("GET", self.base + "/api/subsets/1")
        self.assertEqual(status, 405)
        status, _ = request("POST", self.base + "/api/subsets/1", {})
        self.assertEqual(status, 405)

    def test_duplicate_name_rejected(self):
        made = self.make_subset()
        status, _ = request("POST", self.base + "/api/subsets", {"name": made["name"]})
        self.assertEqual(status, 409)

    def test_rename_collision_rejected(self):
        first = self.make_subset()
        second = self.make_subset()
        status, _ = request(
            "PUT",
            "%s/api/subsets/%d" % (self.base, second["id"]),
            {"name": first["name"]},
        )
        self.assertEqual(status, 409)

    def test_static_pages_served(self):
        status, body = request("GET", self.base + "/picker/picker.html")
        self.assertEqual(status, 200)
        self.assertTrue(body)

    def test_var_stays_unserved(self):
        status, _ = request("GET", self.base + "/var/subsets.db")
        self.assertEqual(status, 404)

    def test_no_journal_files_linger(self):
        self.make_subset()
        request("GET", self.base + "/api/subsets")
        for suffix in ("-journal", "-wal", "-shm"):
            self.assertFalse(os.path.exists(self.db + suffix), suffix)


if __name__ == "__main__":
    unittest.main()
