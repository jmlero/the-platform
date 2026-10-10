import importlib.util
import json
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parents[1]


class KagentPreparationTest(unittest.TestCase):
    def test_upgrade_removes_retired_fields_without_changing_authority_or_credentials(self):
        spec = importlib.util.spec_from_file_location("prepare_kagent", ROOT / "scripts/prepare-kagent-installation.py")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        config = json.loads((ROOT / "deploy/kubernetes/first-release/workflow.example.json").read_text())
        config["analysis"] = {"revision": 7, "image": "old", "tools": ["read_context_source"]}
        config["analysis_engine"] = {"backend": "retired"}
        original = json.loads(json.dumps(config))
        image = "verifier@sha256:" + "a" * 64
        prepared = module.prepare(config, image)
        expected = json.loads(json.dumps(config))
        expected.pop("analysis")
        expected.pop("analysis_engine")
        expected["kubernetes"]["image"] = image
        self.assertEqual(prepared, expected)
        self.assertEqual(config, original)
        self.assertEqual(module.prepare(prepared, image), prepared)
        with self.assertRaises(ValueError):
            module.prepare(config, "verifier:latest")
        config.pop("repository_engine")
        with self.assertRaises(ValueError):
            module.prepare(config, image)


if __name__ == "__main__":
    unittest.main()
