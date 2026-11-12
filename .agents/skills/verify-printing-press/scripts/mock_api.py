#!/usr/bin/env python3
import http.server
import json
import pathlib
import socketserver
import sys

port_file = pathlib.Path(sys.argv[1])
request_log = pathlib.Path(sys.argv[2])

PROJECT = {"id": "proj-1", "name": "Alpha", "status": "active", "updated_at": "2026-01-01T00:00:00Z"}
TASK = {"id": "task-1", "title": "First", "status": "open", "assignee": "ada", "updated_at": "2026-01-01T00:00:00Z"}


class Handler(http.server.BaseHTTPRequestHandler):
    def _record(self, body):
        entry = {
            "method": self.command,
            "path": self.path,
            "x_api_key": self.headers.get("X-API-Key"),
            "body": body,
        }
        with request_log.open("a") as f:
            f.write(json.dumps(entry) + "\n")

    def _send(self, status, payload):
        data = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def _body(self):
        n = int(self.headers.get("content-length") or 0)
        raw = self.rfile.read(n) if n else b""
        try:
            return json.loads(raw) if raw else None
        except ValueError:
            return raw.decode(errors="replace")

    def _route(self):
        body = self._body()
        self._record(body)
        p = self.path.split("?", 1)[0]
        if p.endswith("/public/status"):
            return self._send(200, {"status": "ok"})
        if p.endswith("/currencies"):
            return self._send(200, [{"code": "USD", "name": "US Dollar"}])
        if p.endswith("/projects") and self.command == "GET":
            return self._send(200, {"data": [PROJECT], "next_cursor": None})
        if p.endswith("/projects") and self.command == "POST":
            return self._send(201, dict(PROJECT, id="proj-new", **(body if isinstance(body, dict) else {})))
        if p.endswith("/tasks") and self.command == "GET":
            return self._send(200, {"data": [TASK], "next_cursor": None})
        if "/projects/" in p and self.command == "GET":
            return self._send(200, PROJECT)
        return self._send(200, {"ok": True})

    do_GET = do_POST = do_PUT = do_PATCH = do_DELETE = _route

    def log_message(self, fmt, *args):
        return


with socketserver.TCPServer(("127.0.0.1", 0), Handler) as httpd:
    port_file.write_text(str(httpd.server_address[1]))
    httpd.serve_forever()
