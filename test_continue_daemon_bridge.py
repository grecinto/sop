import importlib.util
import json
import os
import tempfile
import time
import unittest
from pathlib import Path


def load_bridge_module():
    script_path = Path(__file__).with_name("continue-daemon-bridge.py")
    spec = importlib.util.spec_from_file_location("continue_daemon_bridge", script_path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class ContinueDaemonBridgeTests(unittest.TestCase):
    def setUp(self):
        self.module = load_bridge_module()
        self.tmpdir = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmpdir.cleanup)
        self.module.AUTH_TOKEN = ""
        self.module.REFRESH_TOKEN = ""
        self.module.PROVIDER = ""
        self.module.MODEL = ""
        self.module.API_KEY = ""
        self.module.LLM_URL = ""
        self.module.ACTIVE_FILE = "README.md"
        self.module.WORKSPACE_ROOT = "/tmp/workspace"
        self.module.SESSION_ID = "continue-test"
        self.module.REQUESTED_PHASE = "execution"
        self.module.TOOL_CHOICE = "apply_patch"
        self.module.ALLOWED_TOOLS = ["read_file", "apply_patch", "get_problems"]
        self.module.TEAM_PRACTICES = ["Preserve public interfaces", "Add targeted tests"]
        self.module.TOKEN_CACHE_PATH = os.path.join(self.tmpdir.name, "continue-helper-session.json")
        self.module.BOOTSTRAP_PATH = os.path.join(self.tmpdir.name, "daemon.bootstrap")

    def test_get_access_token_uses_valid_cached_token_without_network(self):
        self.module.save_token_cache({
            "access_token": "cached-token",
            "refresh_token": "cached-refresh",
            "expires_at": time.time() + 300,
        })

        calls = []

        def fail_post_json_raw(path, payload, include_auth=True):
            calls.append((path, payload, include_auth))
            raise AssertionError("network should not be used when cache is still valid")

        self.module.post_json_raw = fail_post_json_raw

        token = self.module.get_access_token()

        self.assertEqual(token, "cached-token")
        self.assertEqual(calls, [])

    def test_get_access_token_refreshes_expired_cache_and_updates_token_file(self):
        self.module.save_token_cache({
            "access_token": "expired-token",
            "refresh_token": "refresh-me",
            "expires_at": time.time() - 10,
        })

        calls = []

        def fake_post_json_raw(path, payload, include_auth=True):
            calls.append((path, payload, include_auth))
            self.assertEqual(path, "/api/daemon/refresh")
            self.assertEqual(payload, {"refresh_token": "refresh-me"})
            self.assertFalse(include_auth)
            return 200, json.dumps({
                "access_token": "refreshed-token",
                "refresh_token": "refreshed-refresh",
                "expires_in": 600,
            })

        self.module.post_json_raw = fake_post_json_raw

        token = self.module.get_access_token()

        self.assertEqual(token, "refreshed-token")
        self.assertEqual(len(calls), 1)
        cached = self.module.load_token_cache()
        self.assertEqual(cached["access_token"], "refreshed-token")
        self.assertEqual(cached["refresh_token"], "refreshed-refresh")
        self.assertGreater(cached["expires_at"], time.time())

    def test_get_access_token_bootstraps_when_refresh_is_unavailable(self):
        with open(self.module.BOOTSTRAP_PATH, "w", encoding="utf-8") as fh:
            fh.write("bootstrap-secret")

        calls = []

        def fake_post_json_raw(path, payload, include_auth=True):
            calls.append((path, payload, include_auth))
            self.assertEqual(path, "/api/daemon/auth")
            self.assertEqual(payload, {"bootstrap_secret": "bootstrap-secret"})
            self.assertFalse(include_auth)
            return 200, json.dumps({
                "access_token": "bootstrap-token",
                "refresh_token": "bootstrap-refresh",
                "expires_in": 120,
            })

        self.module.post_json_raw = fake_post_json_raw

        token = self.module.get_access_token()

        self.assertEqual(token, "bootstrap-token")
        self.assertEqual(len(calls), 1)
        cached = self.module.load_token_cache()
        self.assertEqual(cached["refresh_token"], "bootstrap-refresh")

    def test_continue_execute_payload_preserves_request_scoped_provider_details(self):
        self.module.PROVIDER = "openai"
        self.module.MODEL = "gpt-5.4"
        self.module.API_KEY = "continue-key"
        self.module.LLM_URL = "https://api.openai.com"
        self.module.ACTIVE_FILE = "src/main.go"
        self.module.WORKSPACE_ROOT = "/workspace/repo"
        self.module.SESSION_ID = "workspace-42"

        payload = self.module.continue_execute_payload("Apply the prepared patch")

        self.assertEqual(payload["session_id"], "workspace-42")
        self.assertEqual(payload["task"], "Apply the prepared patch")
        self.assertEqual(payload["mode"], "apply")
        self.assertEqual(payload["provider"], "openai")
        self.assertEqual(payload["model"], "gpt-5.4")
        self.assertEqual(payload["api_key"], "continue-key")
        self.assertEqual(payload["url"], "https://api.openai.com")
        self.assertEqual(payload["requested_phase"], "execution")
        self.assertEqual(payload["allowed_tools"], ["read_file", "apply_patch", "get_problems"])
        self.assertEqual(payload["tool_choice"], "apply_patch")
        self.assertEqual(payload["guidance"]["team_practices"], ["Preserve public interfaces", "Add targeted tests"])
        self.assertEqual(payload["guidance"]["expected_result_format"], "structured_execution_result")
        self.assertTrue(payload["result_contract"]["stream_progress"])
        self.assertTrue(payload["result_contract"]["include_modified_files"])
        self.assertTrue(payload["result_contract"]["prefer_artifact_refs"])
        self.assertEqual(payload["result_contract"]["max_inline_diff_bytes"], 16384)
        self.assertEqual(payload["context"]["workspace_root"], "/workspace/repo")
        self.assertEqual(payload["context"]["active_file"], "src/main.go")
        self.assertEqual(payload["context"]["open_files"], ["src/main.go"])

    def test_build_worker_response_emits_structured_execution_result(self):
        self.module.ACTIVE_FILE = "src/main.go"

        events = self.module.build_worker_response({
            "task": "Apply the prepared patch",
            "mode": "apply",
            "requested_phase": "execution",
            "tool_choice": "apply_patch",
            "allowed_tools": ["read_file", "apply_patch"],
            "guidance": {"team_practices": ["Use existing logger"]},
            "provider": "openai",
            "model": "gpt-5.4",
            "context": {"active_file": "src/main.go"},
            "result_contract": {
                "stream_progress": True,
                "include_modified_files": True,
                "include_diff_deltas": True,
                "include_metadata": True,
                "prefer_artifact_refs": True,
                "max_inline_diff_bytes": 16384,
            },
        })

        event_types = [event["type"] for event in events]
        self.assertIn("progress", event_types)
        self.assertIn("continue_progress", event_types)
        self.assertIn("continue_modified_file", event_types)
        self.assertEqual(events[-1]["type"], "done")
        result = events[-1]["result"]
        self.assertEqual(result["status"], "completed")
        self.assertEqual(result["metadata"]["requested_phase"], "execution")
        self.assertEqual(result["metadata"]["tool_choice"], "apply_patch")
        self.assertEqual(result["metadata"]["guidance"]["team_practices"], ["Use existing logger"])
        self.assertEqual(result["modified_files"][0]["file_path"], "src/main.go")
        self.assertEqual(result["modified_files"][0]["crud_operation"], "update")
        self.assertEqual(result["modified_files"][0]["diff_format"], "unified")
        self.assertIn("diff_delta", result["modified_files"][0])


if __name__ == "__main__":
    unittest.main()