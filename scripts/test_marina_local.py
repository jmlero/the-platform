import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("marina_local", Path(__file__).with_name("marina-local.py"))
local = importlib.util.module_from_spec(spec)
spec.loader.exec_module(local)
VERIFIER = "localhost:5000/the-platform/marina-verifier@sha256:" + "a" * 64


class WorkflowTests(unittest.TestCase):
    def test_without_accounts_marina_chats_through_the_fake_model(self):
        config = local.workflow(model=None, github=None)
        self.assertEqual(config["model"]["endpoint"], "https://fake-model.platform-gateway.svc/v1")
        self.assertEqual(config["git"], {})
        self.assertNotIn("repository_engine", config)
        self.assertEqual(config["conversation"]["engine"]["harness"], "kint-conversation-byo")

    def test_a_model_key_routes_marina_through_agentgateway(self):
        config = local.workflow(model="openai/gpt-5-mini", github=None)
        self.assertEqual(config["model"]["endpoint"], "https://agentgateway.platform-gateway.svc/v1")
        self.assertEqual(config["model"]["name"], "openai/gpt-5-mini")
        self.assertEqual(config["model"]["token_file"], "/run/marina-secrets/agentgateway-client-key")

    def test_github_mode_adds_repository_work_with_a_different_reviewer(self):
        github = {"repository": "me/app", "checks": ["npm test"], "paths": ["src", "tests"], "verifier": VERIFIER}
        config = local.workflow(model="openai/gpt-5-mini", github=github)
        self.assertEqual(config["git"]["clone_url"], "https://github.com/me/app.git")
        self.assertEqual(config["repository_engine"]["harness"], "kint-codex")
        self.assertEqual(config["kubernetes"]["image"], VERIFIER)
        self.assertEqual(config["checks"], [{"id": "check-1", "directory": ".", "command": ["sh", "-c", "npm test"]}])
        self.assertNotEqual(config["reviewer_model"]["name"], config["model"]["name"])
        self.assertEqual(config["allowed_paths"], ["src", "tests"])

    def test_github_mode_needs_a_real_model(self):
        with self.assertRaises(ValueError):
            local.workflow(model=None, github={"repository": "me/app", "checks": ["true"], "paths": ["src"], "verifier": VERIFIER})

    def test_every_mode_passes_marinas_own_validation(self):
        marina = ROOT / "marina/bin/marina"
        if not marina.exists():
            self.skipTest("build Marina first: make -C marina build")
        github = {"repository": "me/app", "checks": ["npm test"], "paths": ["src", "tests"], "verifier": VERIFIER}
        for config in (local.workflow(None, None), local.workflow("openai/gpt-5-mini", None),
                       local.workflow("openai/gpt-5-mini", github)):
            with tempfile.NamedTemporaryFile("w", suffix=".json") as file:
                json.dump(config, file)
                file.flush()
                result = subprocess.run([str(marina), "check-config", "--config", file.name],
                                        capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)


class RegistryTests(unittest.TestCase):
    def test_node_pulls_from_the_local_registry_over_http(self):
        self.assertEqual(local.hosts_toml("10.89.1.2:5000"),
                         '[host."http://10.89.1.2:5000"]\n  capabilities = ["pull", "resolve"]\n')


if __name__ == "__main__":
    unittest.main()
