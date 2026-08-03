#!/usr/bin/env python3
import json
import sys
import urllib.request
from urllib.error import HTTPError, URLError

BASE = "http://127.0.0.1:8080"


def post_json(path, payload):
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(payload).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=20) as res:
            body = res.read().decode("utf-8")
            return res.status, body
    except HTTPError as e:
        body = e.read().decode("utf-8", "replace")
        return e.code, body
    except URLError as e:
        return None, str(e.reason)


def main():
    if len(sys.argv) < 2:
        print("Usage: python3 continue-daemon-bridge.py <chat|read|patch|test>")
        return

    mode = sys.argv[1]
    if mode == "chat":
        payload = {
            "session_id": "continue-test",
            "message": "Inspect the repository and tell me what files are present.",
            "workspace": {"root": "/Volumes/T7/sop-cloud", "active_file": "README.md"},
            "context": {"open_files": ["README.md"], "selected_text": ""},
            "tools": [{"name": "repo.read_file"}, {"name": "test.run"}],
            "mode": "editor",
        }
        status, body = post_json("/api/continue/chat", payload)
        print("STATUS", status)
        print(body)
    elif mode == "read":
        payload = {"path": "/Volumes/T7/sop-cloud/README.md", "start_line": 1, "end_line": 20}
        status, body = post_json("/api/continue/tools/repo.read_file", payload)
        print("STATUS", status)
        print(body)
    elif mode == "patch":
        payload = {"path": "/Volumes/T7/sop-cloud/README.md", "selection": "after"}
        status, body = post_json("/api/continue/tools/code.apply_patch", payload)
        print("STATUS", status)
        print(body)
    elif mode == "test":
        payload = {"query": "echo continue-bridge-test", "session_id": "continue-test"}
        status, body = post_json("/api/continue/tools/test.run", payload)
        print("STATUS", status)
        print(body)
    else:
        print("Unknown mode")


if __name__ == "__main__":
    main()
