# loopback static server for the picker and guide pages: only the allowlisted
# page trees are served, so var/ (stratz token, duckdb state) and .git stay
# unreachable even from this machine. make serve passes the port.
import os
import sys
from http.server import HTTPServer, SimpleHTTPRequestHandler

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ALLOWED = ("guide", "picker", "ui")


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


def main():
    if len(sys.argv) != 2:
        sys.exit("usage: serve.py PORT")
    HTTPServer(("127.0.0.1", int(sys.argv[1])), Handler).serve_forever()


if __name__ == "__main__":
    main()
