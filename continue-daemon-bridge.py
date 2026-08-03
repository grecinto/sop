#!/usr/bin/env python3
import argparse
import json
import os
import socketserver
import sys
import time
import urllib.request
from http.server import BaseHTTPRequestHandler
from http.server import HTTPServer
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse

BASE = os.environ.get("SOP_CONTINUE_BASE_URL", "http://127.0.0.1:9090").rstrip("/")
AUTH_TOKEN = os.environ.get("SOP_CONTINUE_BEARER_TOKEN", "").strip()
REFRESH_TOKEN = os.environ.get("SOP_CONTINUE_REFRESH_TOKEN", "").strip()
PROVIDER = os.environ.get("SOP_CONTINUE_PROVIDER", "").strip()
MODEL = os.environ.get("SOP_CONTINUE_MODEL", "").strip()
API_KEY = os.environ.get("SOP_CONTINUE_API_KEY", "").strip()
LLM_URL = os.environ.get("SOP_CONTINUE_LLM_URL", "").strip()
WORKSPACE_ROOT = os.environ.get("SOP_CONTINUE_WORKSPACE_ROOT", "/Volumes/T7/sop-cloud")
ACTIVE_FILE = os.environ.get("SOP_CONTINUE_ACTIVE_FILE", "README.md")
SESSION_ID = os.environ.get("SOP_CONTINUE_SESSION_ID", "continue-test")
CONFIG_DIR = os.path.join(os.path.expanduser("~"), ".config", "sop")
BOOTSTRAP_PATH = os.environ.get("SOP_CONTINUE_BOOTSTRAP_PATH", os.path.join(CONFIG_DIR, "daemon.bootstrap"))
TOKEN_CACHE_PATH = os.environ.get("SOP_CONTINUE_TOKEN_CACHE", os.path.join(CONFIG_DIR, "continue-helper-session.json"))
WORKER_IDE = os.environ.get("SOP_CONTINUE_WORKER_IDE", "vscode").strip() or "vscode"
REQUESTED_PHASE = os.environ.get("SOP_CONTINUE_REQUESTED_PHASE", "execution").strip() or "execution"
TOOL_CHOICE = os.environ.get("SOP_CONTINUE_TOOL_CHOICE", "apply_patch").strip()
ALLOWED_TOOLS = [
    tool.strip()
    for tool in os.environ.get(
        "SOP_CONTINUE_ALLOWED_TOOLS",
        "read_file,apply_patch,get_problems,run_terminal_command",
    ).split(",")
    if tool.strip()
]
WORKER_CAPABILITIES = [
    cap.strip()
    for cap in os.environ.get(
        "SOP_CONTINUE_WORKER_CAPABILITIES",
        "apply_patch,read_context,edit_code,diff_review,plan_changes",
    ).split(",")
    if cap.strip()
]
TEAM_PRACTICES = [
    item.strip()
    for item in os.environ.get(
        "SOP_CONTINUE_TEAM_PRACTICES",
        "Preserve public interfaces,Add targeted tests",
    ).split(",")
    if item.strip()
]


class ThreadedHTTPServer(socketserver.ThreadingMixIn, HTTPServer):
    daemon_threads = True


def build_headers():
    headers = {"Content-Type": "application/json"}
    token = get_access_token()
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def load_token_cache():
    if not os.path.exists(TOKEN_CACHE_PATH):
        return {}
    try:
        with open(TOKEN_CACHE_PATH, "r", encoding="utf-8") as fh:
            return json.load(fh) or {}
    except Exception:
        return {}


def save_token_cache(payload):
    if not payload:
        return
    os.makedirs(os.path.dirname(TOKEN_CACHE_PATH), exist_ok=True)
    with open(TOKEN_CACHE_PATH, "w", encoding="utf-8") as fh:
        json.dump(payload, fh)


def clear_token_cache():
    if os.path.exists(TOKEN_CACHE_PATH):
        os.remove(TOKEN_CACHE_PATH)


def cache_is_valid(cache):
    expires_at = float(cache.get("expires_at", 0) or 0)
    if not cache.get("access_token"):
        return False
    if expires_at <= 0:
        return True
    return time.time() < max(0, expires_at - 30)


def read_bootstrap_secret():
    if not os.path.exists(BOOTSTRAP_PATH):
        return ""
    with open(BOOTSTRAP_PATH, "r", encoding="utf-8") as fh:
        return fh.read().strip()


def daemon_auth_with_bootstrap_secret():
    secret = read_bootstrap_secret()
    if not secret:
        return None, "bootstrap secret not found"
    status, body = post_json_raw(
        "/api/daemon/auth",
        {"bootstrap_secret": secret},
        include_auth=False,
    )
    if status != 200:
        return None, body
    try:
        payload = json.loads(body)
    except json.JSONDecodeError:
        return None, body
    persist_tokens(payload)
    return payload.get("access_token"), None


def daemon_refresh(refresh_token):
    status, body = post_json_raw(
        "/api/daemon/refresh",
        {"refresh_token": refresh_token},
        include_auth=False,
    )
    if status != 200:
        return None, body
    try:
        payload = json.loads(body)
    except json.JSONDecodeError:
        return None, body
    persist_tokens(payload)
    return payload.get("access_token"), None


def persist_tokens(payload):
    access_token = str(payload.get("access_token", "") or "").strip()
    refresh_token = str(payload.get("refresh_token", "") or "").strip()
    expires_in = int(payload.get("expires_in", 0) or 0)
    cache_payload = {
        "access_token": access_token,
        "refresh_token": refresh_token,
        "expires_at": time.time() + expires_in if expires_in > 0 else 0,
    }
    save_token_cache(cache_payload)


def get_access_token():
    if AUTH_TOKEN:
        return AUTH_TOKEN
    cache = load_token_cache()
    if cache_is_valid(cache):
        return cache.get("access_token", "")
    refresh_token = REFRESH_TOKEN or str(cache.get("refresh_token", "") or "").strip()
    if refresh_token:
        token, err = daemon_refresh(refresh_token)
        if token and not err:
            return token
    token, err = daemon_auth_with_bootstrap_secret()
    if token and not err:
        return token
    return ""


def post_json_raw(path, payload, include_auth=True):
    headers = {"Content-Type": "application/json"}
    if include_auth:
        token = get_access_token()
        if token:
            headers["Authorization"] = f"Bearer {token}"
    req = urllib.request.Request(
        BASE + path,
        data=json.dumps(payload).encode("utf-8"),
        headers=headers,
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


def post_json(path, payload):
    return post_json_raw(path, payload, include_auth=True)


def get(path):
    req = urllib.request.Request(BASE + path, headers=build_headers(), method="GET")
    try:
        with urllib.request.urlopen(req, timeout=20) as res:
            body = res.read().decode("utf-8")
            return res.status, body
    except HTTPError as e:
        body = e.read().decode("utf-8", "replace")
        return e.code, body
    except URLError as e:
        return None, str(e.reason)


def print_response(status, body):
    print("STATUS", status)
    print(body)


def register_worker(callback_url, capabilities):
    payload = {
        "session_id": SESSION_ID,
        "callback_url": callback_url,
        "capabilities": capabilities,
        "ide": WORKER_IDE,
    }
    return post_json("/api/continue/worker/register", payload)


def worker_state():
    return get(f"/api/continue/workers/{SESSION_ID}")


def build_worker_response(task_request):
    task = str(task_request.get("task", "")).strip()
    mode = str(task_request.get("mode", "")).strip() or "edit"
    provider = str(task_request.get("provider", "")).strip()
    model = str(task_request.get("model", "")).strip()
    requested_phase = str(task_request.get("requested_phase", "")).strip() or REQUESTED_PHASE
    tool_choice = str(task_request.get("tool_choice", "")).strip()
    allowed_tools = list(task_request.get("allowed_tools") or [])
    guidance = dict(task_request.get("guidance") or {})
    active_file = str(task_request.get("context", {}).get("active_file", "")).strip()
    target_label = active_file or ACTIVE_FILE
    result_contract = dict(task_request.get("result_contract") or {})
    max_inline_diff_bytes = int(result_contract.get("max_inline_diff_bytes") or 0)
    prefer_artifact_refs = bool(result_contract.get("prefer_artifact_refs"))

    diff_delta = "@@ -1,2 +1,4 @@\n-// TODO: implement\n+// Continue Ask2Ask worker applied delegated change\n+func delegatedChange() error {\n+    return nil\n+}"
    diff_artifact_ref = ""
    inline_diff = diff_delta
    if prefer_artifact_refs and max_inline_diff_bytes > 0 and len(diff_delta.encode("utf-8")) > max_inline_diff_bytes:
        diff_artifact_ref = f"artifacts/diffs/{os.path.basename(target_label)}.patch"
        inline_diff = ""

    modified_file = {
        "file_path": target_label,
        "crud_operation": "update",
        "diff_summary": f"Applied delegated {mode} change to {target_label}",
        "diff_format": "unified",
        "metadata": {
            "phase": requested_phase,
            "mode": mode,
            "provider": provider or "default",
            "model": model or "default",
        },
    }
    if inline_diff:
        modified_file["diff_delta"] = inline_diff
    if diff_artifact_ref:
        modified_file["diff_artifact_ref"] = diff_artifact_ref

    result = {
        "status": "completed",
        "summary": f"Continue completed delegated {requested_phase} work for {target_label}",
        "modified_files": [modified_file],
        "metadata": {
            "task": task,
            "mode": mode,
            "requested_phase": requested_phase,
            "tool_choice": tool_choice,
            "allowed_tools": allowed_tools,
            "guidance": guidance,
        },
    }
    if diff_artifact_ref:
        result["artifacts"] = [{
            "id": f"diff::{os.path.basename(target_label)}",
            "kind": "diff",
            "path": diff_artifact_ref,
            "description": f"Full diff for {target_label}",
            "metadata": {"diff_format": "unified"},
        }]

    events = [
        {"type": "progress", "message": f"Continue worker accepted {mode} task"},
        {
            "type": "continue_progress",
            "message": f"Continue Ask2Ask handling phase={requested_phase} file={target_label}",
        },
        {
            "type": "tool_call",
            "name": "continue.worker.execute",
            "args": {
                "task": task,
                "mode": mode,
                "requested_phase": requested_phase,
                "active_file": target_label,
                "provider": provider,
                "model": model,
                "tool_choice": tool_choice,
                "allowed_tools": allowed_tools,
                "guidance": guidance,
            },
        },
        {
            "type": "content",
            "delta": (
                f"Continue worker simulated {mode} on {target_label}. "
                f"Provider={provider or 'default'} Model={model or 'default'}"
            ),
        },
        {
            "type": "continue_modified_file",
            "file": modified_file,
        },
        {
            "type": "done",
            "result": result,
        },
    ]
    return events


def make_worker_handler(verbose):
    class ContinueWorkerHandler(BaseHTTPRequestHandler):
        server_version = "SOPContinueWorker/1.0"

        def log_message(self, fmt, *args):
            if verbose:
                super().log_message(fmt, *args)

        def do_POST(self):
            parsed = urlparse(self.path)
            if parsed.path != "/continue/worker":
                self.send_response(404)
                self.send_header("Content-Type", "text/plain; charset=utf-8")
                self.end_headers()
                self.wfile.write(b"not found")
                return

            length = int(self.headers.get("Content-Length", "0") or "0")
            raw_body = self.rfile.read(length) if length > 0 else b"{}"
            try:
                task_request = json.loads(raw_body.decode("utf-8"))
            except json.JSONDecodeError as exc:
                self.send_response(400)
                self.send_header("Content-Type", "text/plain; charset=utf-8")
                self.end_headers()
                self.wfile.write(f"invalid json: {exc}".encode("utf-8"))
                return

            print("WORKER REQUEST")
            print(json.dumps(task_request, indent=2))

            self.send_response(200)
            self.send_header("Content-Type", "application/x-ndjson")
            self.end_headers()

            for event in build_worker_response(task_request):
                line = json.dumps(event).encode("utf-8") + b"\n"
                self.wfile.write(line)
                self.wfile.flush()

    return ContinueWorkerHandler


def serve_worker(port, auto_register, verbose):
    callback_url = f"http://127.0.0.1:{port}/continue/worker"
    if auto_register:
        status, body = register_worker(callback_url, WORKER_CAPABILITIES)
        print_response(status, body)

    handler = make_worker_handler(verbose)
    server = ThreadedHTTPServer(("127.0.0.1", port), handler)
    print(f"Continue worker listening on {callback_url}")
    print(f"Session: {SESSION_ID}")
    print(f"Capabilities: {', '.join(WORKER_CAPABILITIES)}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


def parse_args():
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("mode", nargs="?", default="")
    parser.add_argument("extra", nargs="?")
    parser.add_argument("--register", action="store_true")
    parser.add_argument("--verbose", action="store_true")
    return parser.parse_args()


def continue_chat_payload(message):
    payload = {
        "session_id": SESSION_ID,
        "message": message,
        "workspace": {"root": WORKSPACE_ROOT, "active_file": ACTIVE_FILE},
        "context": {"open_files": [ACTIVE_FILE], "selected_text": ""},
        "tools": [{"name": "repo.read_file"}, {"name": "test.run"}],
        "mode": "editor",
    }
    if PROVIDER:
        payload["provider"] = PROVIDER
    if MODEL:
        payload["model"] = MODEL
    if API_KEY:
        payload["api_key"] = API_KEY
    if LLM_URL:
        payload["url"] = LLM_URL
    return payload


def continue_execute_payload(task):
    payload = {
        "session_id": SESSION_ID,
        "task": task,
        "mode": "apply",
        "requested_phase": REQUESTED_PHASE,
        "allowed_tools": ALLOWED_TOOLS,
        "guidance": {
            "team_practices": TEAM_PRACTICES,
            "success_criteria": ["Return structured execution result", "Keep changes bounded"],
            "constraints": ["Preserve public interfaces"],
            "expected_result_format": "structured_execution_result",
        },
        "context": {
            "workspace_root": WORKSPACE_ROOT,
            "active_file": ACTIVE_FILE,
            "selected_text": "",
            "open_files": [ACTIVE_FILE],
        },
        "result_contract": {
            "stream_progress": True,
            "include_modified_files": True,
            "include_diff_deltas": True,
            "include_metadata": True,
            "prefer_artifact_refs": True,
            "max_inline_diff_bytes": 16384,
        },
    }
    if TOOL_CHOICE:
        payload["tool_choice"] = TOOL_CHOICE
    if PROVIDER:
        payload["provider"] = PROVIDER
    if MODEL:
        payload["model"] = MODEL
    if API_KEY:
        payload["api_key"] = API_KEY
    if LLM_URL:
        payload["url"] = LLM_URL
    return payload


def main():
    args = parse_args()
    if not args.mode:
        print(
            "Usage: python3 continue-daemon-bridge.py "
            "<auth|chat|execute|read|patch|test|metadata|session|worker-state|register-worker|serve-worker>"
        )
        return

    mode = args.mode
    if mode == "auth":
        token = get_access_token()
        if token:
            cache = load_token_cache()
            print("STATUS 200")
            print(json.dumps({
                "authenticated": True,
                "token_cache": TOKEN_CACHE_PATH,
                "has_refresh_token": bool(cache.get("refresh_token")),
                "bootstrap_path": BOOTSTRAP_PATH,
            }, indent=2))
        else:
            print("STATUS 401")
            print(json.dumps({
                "authenticated": False,
                "token_cache": TOKEN_CACHE_PATH,
                "bootstrap_path": BOOTSTRAP_PATH,
            }, indent=2))
    elif mode == "chat":
        payload = continue_chat_payload("Inspect the repository and tell me what files are present.")
        status, body = post_json("/api/continue/chat", payload)
        print_response(status, body)
    elif mode == "execute":
        task = args.extra or f"Apply the prepared patch for {ACTIVE_FILE}"
        payload = continue_execute_payload(task)
        status, body = post_json("/api/continue/agent/execute", payload)
        print_response(status, body)
    elif mode == "read":
        payload = {"path": os.path.join(WORKSPACE_ROOT, ACTIVE_FILE), "start_line": 1, "end_line": 20}
        status, body = post_json("/api/continue/tools/repo.read_file", payload)
        print_response(status, body)
    elif mode == "patch":
        payload = {"path": os.path.join(WORKSPACE_ROOT, ACTIVE_FILE), "selection": "after"}
        status, body = post_json("/api/continue/tools/code.apply_patch", payload)
        print_response(status, body)
    elif mode == "test":
        payload = {"query": "echo continue-bridge-test", "session_id": SESSION_ID}
        status, body = post_json("/api/continue/tools/test.run", payload)
        print_response(status, body)
    elif mode == "metadata":
        status, body = get("/api/continue/metadata")
        print_response(status, body)
    elif mode == "session":
        status, body = get(f"/api/continue/sessions/{SESSION_ID}")
        print_response(status, body)
    elif mode == "worker-state":
        status, body = worker_state()
        print_response(status, body)
    elif mode == "register-worker":
        callback_url = args.extra or os.environ.get("SOP_CONTINUE_WORKER_CALLBACK_URL", "http://127.0.0.1:65432/continue/worker")
        status, body = register_worker(callback_url, WORKER_CAPABILITIES)
        print_response(status, body)
    elif mode == "serve-worker":
        port = int(args.extra or os.environ.get("SOP_CONTINUE_WORKER_PORT", "65432"))
        serve_worker(port, args.register, args.verbose)
    else:
        print("Unknown mode")


if __name__ == "__main__":
    main()
