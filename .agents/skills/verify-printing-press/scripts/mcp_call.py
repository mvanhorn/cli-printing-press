#!/usr/bin/env python3
import json
import subprocess
import sys

binary, action = sys.argv[1], sys.argv[2]
proc = subprocess.Popen([binary], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
_next_id = 0


def _send(msg):
    proc.stdin.write(json.dumps(msg) + "\n")
    proc.stdin.flush()


def notify(method):
    _send({"jsonrpc": "2.0", "method": method})


def rpc(method, params=None):
    global _next_id
    msg = {"jsonrpc": "2.0", "method": method}
    if params is not None:
        msg["params"] = params
    _next_id += 1
    msg["id"] = _next_id
    _send(msg)
    while True:
        line = proc.stdout.readline()
        if not line:
            sys.exit(f"mcp server closed stdout while waiting for {method}")
        resp = json.loads(line)
        if resp.get("id") == msg["id"]:
            if "error" in resp:
                sys.exit(json.dumps(resp["error"]))
            return resp["result"]


try:
    rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                       "clientInfo": {"name": "verify-harness", "version": "0"}})
    notify("notifications/initialized")
    if action == "list":
        tools, cursor = [], None
        while True:
            res = rpc("tools/list", {"cursor": cursor} if cursor else {})
            tools += res.get("tools", [])
            cursor = res.get("nextCursor")
            if not cursor:
                break
        print(json.dumps([{"name": t["name"], "annotations": t.get("annotations", {})} for t in tools], indent=1))
    elif action == "call":
        print(json.dumps(rpc("tools/call", {"name": sys.argv[3], "arguments": json.loads(sys.argv[4])}), indent=1))
    else:
        sys.exit(f"unknown action {action}")
finally:
    proc.stdin.close()
    proc.wait(timeout=10)
