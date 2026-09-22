import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class AgentPreparationTest(unittest.TestCase):
    def test_upgrade_preserves_repository_policy_and_credentials(self):
        spec = importlib.util.spec_from_file_location("prepare_agent", ROOT / "scripts/prepare-agent-installation.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        config = json.loads((ROOT / "deploy/kubernetes/first-release/workflow.example.json").read_text())
        auth = {"identities": [{"id": "marina", "projects": ["prj_default"], "token_sha256": ["unchanged"], "capabilities": ["context.read"]}]}
        original = json.loads(json.dumps(config))
        image = "analysis@sha256:" + "a" * 64
        new, policy = module.prepare(config, auth, image, "marina")
        self.assertEqual({k: v for k, v in new.items() if k not in ("analysis", "version")}, original)
        self.assertEqual(policy["identities"][0]["token_sha256"], ["unchanged"])
        self.assertIn("workload.publish", policy["identities"][0]["capabilities"])
        self.assertEqual(new["analysis"]["tools"], ["read_context_source"])
        self.assertEqual(new["analysis"]["harness_version"], "langchain-create-agent/v2")
        self.assertEqual(module.prepare(new, policy, image, "marina"), (new, policy))
        changed, _ = module.prepare(new, policy, "analysis@sha256:" + "b" * 64, "marina")
        self.assertEqual(changed["analysis"]["revision"], new["analysis"]["revision"] + 1)
        with self.assertRaises(ValueError):
            module.prepare(config, auth, "analysis:latest", "marina")
        self.assertEqual(config, original)


if __name__ == "__main__":
    unittest.main()
