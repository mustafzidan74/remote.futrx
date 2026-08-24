#!/usr/bin/env bash
# Installs the workspace file preview: a read-only static server on
# 127.0.0.1-reachable :__PREVIEW_PORT__ serving /workspace.
#
# It exists because an agent that writes index.html has, until now, produced
# something nobody can look at. The dev-URL proxy assumes a dev server on a
# port; a plain HTML file has none, so the preview answered 502 and the operator
# was told the page was "done".
#
# Deliberately not a dev server: no build, no watch, no execution. It reads
# files and returns them. Anything that needs to run still runs on its own port.
set -euo pipefail

install -d -m 755 /opt/remote

cat > /opt/remote/file-preview.py <<'PY'
"""Static file server for /workspace.

Python's own handler, with three changes that matter:

* it never serves outside the root — the base class already resolves ".."
  away, and the root is pinned rather than taken from the process cwd;
* it sends no-store, because the whole point is looking at a file the agent
  just rewrote and a cached copy defeats that;
* it labels the common web types explicitly. The stock mimetypes table on a
  minimal container image is missing several, and a stylesheet served as
  text/plain renders as an unstyled page — which reads as "the agent broke my
  CSS" rather than "the server mislabelled it".
"""

import functools
import http.server
import mimetypes
import socketserver

ROOT = "/workspace"
PORT = __PREVIEW_PORT__

for extension, kind in {
    ".css": "text/css",
    ".js": "text/javascript",
    ".mjs": "text/javascript",
    ".json": "application/json",
    ".svg": "image/svg+xml",
    ".webp": "image/webp",
    ".avif": "image/avif",
    ".woff": "font/woff",
    ".woff2": "font/woff2",
    ".webmanifest": "application/manifest+json",
}.items():
    mimetypes.add_type(kind, extension)


class Handler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        self.send_header("Cache-Control", "no-store")
        # The page is framed by the platform's preview drawer, which is a
        # different origin. Denying frames outright would blank that drawer.
        self.send_header("X-Content-Type-Options", "nosniff")
        super().end_headers()

    def log_message(self, *_args):
        # The container's journal is for the operator's own program.
        return


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    handler = functools.partial(Handler, directory=ROOT)
    with Server(("0.0.0.0", PORT), handler) as httpd:
        httpd.serve_forever()
PY
chmod 0755 /opt/remote/file-preview.py

cat > /etc/systemd/system/remote-file-preview.service <<'UNIT'
[Unit]
Description=Workspace file preview (read-only static server)
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/python3 /opt/remote/file-preview.py
Restart=always
RestartSec=2
# Read-only by construction, not just by intent: the process cannot write to
# the workspace it is serving.
ProtectSystem=strict
ReadOnlyPaths=/workspace
PrivateTmp=yes
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now remote-file-preview.service
