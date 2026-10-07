# loopback static server for the picker and guide pages, now also serving
# the subsets writer api backed by var/subsets.db. only the allowlisted page
# trees are served, so var/ (stratz token, duckdb state) and .git stay
# unreachable even from this machine, /api/* alone reaches the db. make serve
# passes the port.
import json
import os
import re
import sys
import threading
from http.server import HTTPServer, SimpleHTTPRequestHandler

import subsets

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ALLOWED = ("guide", "picker", "ui")
API_PREFIX = "/api/"
SUBSET_ID_RX = re.compile(r"^/api/subsets/(\d+)$")
MAX_BODY = 1024 * 1024
# the server is single threaded, the lock still keeps mutations serialized
# if that ever changes
api_lock = threading.Lock()


def clean_entries(entries):
    # validates the [{slug, role}] shape, returns None on any bad or
    # duplicate entry so the caller can reject the whole payload
    if not isinstance(entries, list):
        return None
    out, seen = [], set()
    for e in entries:
        if not isinstance(e, dict) or set(e) != {"slug", "role"}:
            return None
        slug, role = e["slug"], e["role"]
        if not isinstance(slug, str) or not slug:
            return None
        if not isinstance(role, str) or not role:
            return None
        if (slug, role) in seen:
            return None
        seen.add((slug, role))
        out.append({"slug": slug, "role": role})
    return out


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, directory=ROOT, **kwargs)

    def translate_path(self, path):
        resolved = super().translate_path(path)
        rel = os.path.relpath(resolved, ROOT)
        if rel == os.curdir or rel.split(os.sep)[0] not in ALLOWED:
            return os.path.join(ROOT, "__not_served__")
        return resolved

    def list_directory(self, path):
        self.send_error(404, "directory listing disabled")
        return None

    def do_GET(self):
        if self.path.startswith(API_PREFIX):
            self.api_route("GET")
        else:
            super().do_GET()

    def do_POST(self):
        self.api_or_static("POST")

    def do_PUT(self):
        self.api_or_static("PUT")

    def do_DELETE(self):
        self.api_or_static("DELETE")

    def api_or_static(self, method):
        if self.path.startswith(API_PREFIX):
            self.api_route(method)
        else:
            self.drain_request_body()
            self.send_error(501, "unsupported method on the static tree")

    def api_route(self, method):
        path = self.path.split("?", 1)[0]
        if path == "/api/subsets":
            if method == "GET":
                return self.reply_json(200, {"subsets": subsets.list_subsets()})
            if method == "POST":
                return self.api_create()
            self.drain_request_body()
            return self.reply_error(405, "method not allowed on /api/subsets")
        m = SUBSET_ID_RX.match(path)
        if m:
            sid = int(m.group(1))
            if method == "PUT":
                return self.api_update(sid)
            if method == "DELETE":
                return self.api_delete(sid)
            self.drain_request_body()
            return self.reply_error(405, "method not allowed on a subset id")
        self.drain_request_body()
        self.reply_error(404, "unknown api path")

    def api_create(self):
        payload, err = self.read_json()
        if err:
            return self.reply_error(400, err)
        name = payload.get("name") if isinstance(payload, dict) else None
        if not isinstance(name, str) or not name.strip():
            return self.reply_error(400, "name must be a non-empty string")
        with api_lock:
            try:
                sid, name = subsets.create_subset(name.strip())
            except subsets.DuplicateNameError:
                return self.reply_error(409, "a subset with that name exists")
        self.reply_json(201, {"id": sid, "name": name})

    def api_update(self, sid):
        payload, err = self.read_json()
        if err:
            return self.reply_error(400, err)
        if not isinstance(payload, dict):
            return self.reply_error(400, "body must be a json object")
        name = payload.get("name")
        if name is not None and (not isinstance(name, str) or not name.strip()):
            return self.reply_error(400, "name must be a non-empty string")
        entries = payload.get("entries")
        if entries is not None:
            entries = clean_entries(entries)
            if entries is None:
                return self.reply_error(400, "entries must be a list of slug+role objects")
        with api_lock:
            try:
                subsets.update_subset(
                    sid, name.strip() if name is not None else None, entries
                )
            except subsets.MissingSubsetError:
                return self.reply_error(404, "no subset with that id")
            except subsets.DuplicateNameError:
                return self.reply_error(409, "a subset with that name exists")
        self.reply_json(200, subsets.get_subset(sid))

    def api_delete(self, sid):
        with api_lock:
            try:
                subsets.delete_subset(sid)
            except subsets.MissingSubsetError:
                return self.reply_error(404, "no subset with that id")
        self.send_response(204)
        self.end_headers()

    def drain_request_body(self):
        # an unread body must be consumed before the reply goes out, else the
        # connection close after the reply races the client send and the
        # error response dies to a connection reset
        try:
            remaining = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            return
        while remaining > 0:
            chunk = self.rfile.read(min(remaining, 65536))
            if not chunk:
                break
            remaining -= len(chunk)

    def read_json(self):
        # returns (payload, error message), payload is None on any rejection
        try:
            length = int(self.headers.get("Content-Length") or 0)
        except ValueError:
            return None, "bad content-length header"
        if length > MAX_BODY:
            self.drain_request_body()
            return None, "body over the size limit"
        raw = self.rfile.read(length)
        try:
            return json.loads(raw.decode("utf-8")), None
        except ValueError:
            return None, "malformed json body"

    def reply_json(self, code, obj):
        body = json.dumps(obj).encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def reply_error(self, code, message):
        self.reply_json(code, {"error": message})


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: serve.py PORT")
    subsets.init_db()
    HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()


if __name__ == "__main__":
    main()
