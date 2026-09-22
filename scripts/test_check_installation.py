import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("check_installation", Path(__file__).with_name("check-installation.py"))


class InstallationPreflightTest(unittest.TestCase):
    def test_analysis_image_and_harness_are_checked(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            config["analysis"] = {"image": "analysis:latest", "harness_version": "langchain-create-agent/v1", "tools": ["read_context_source"]}
            (root / "workflow.json").write_text(json.dumps(config))
            failures = module.check_configuration(root, "")
            self.assertTrue(any("analysis image" in item for item in failures), failures)
            self.assertTrue(any("v2" in item for item in failures), failures)

    def test_missing_model_and_tokens_are_reported_without_secret_values(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            (root / "workflow.json").write_text(json.dumps(config))
            (root / "git-token").write_text("")
            (root / "model-token").write_text("private-test-value")
            (root / "model-token").chmod(0o600)
            failures = module.check_configuration(root, "image: example:latest\n")
            self.assertTrue(any("RUN-03" in item for item in failures))
            self.assertTrue(any("model" in item for item in failures))
            self.assertTrue(any("digest" in item for item in failures))
            self.assertNotIn("private-test-value", json.dumps(failures))


if __name__ == "__main__":
    unittest.main()
