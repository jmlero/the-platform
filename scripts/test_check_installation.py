import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("check_installation", Path(__file__).with_name("check-installation.py"))


class InstallationPreflightTest(unittest.TestCase):

    def test_chat_repository_context_requires_a_project_scoped_read_grant(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            config["conversation"] = {"budget_minutes": 5, "max_tokens": 20000}
            (root / "workflow.json").write_text(json.dumps(config))
            for projects, capabilities, allowed in (
                ([config["project_id"]], ["workload.publish"], False),
                (["another-project"], ["conversation.context.read"], False),
                ([config["project_id"]], ["workload.publish", "conversation.context.read"], True),
            ):
                with self.subTest(projects=projects, capabilities=capabilities):
                    (root / "service-auth.json").write_text(json.dumps({"identities": [
                        {"projects": projects, "capabilities": capabilities}]}))
                    failures = module.check_configuration(root, "")
                    self.assertEqual(any("CHAT-11" in item for item in failures), not allowed, failures)

    def test_each_configured_project_requires_its_own_service_grant(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            second = json.loads(json.dumps(first))
            second["project_id"] = "second-project"
            (root / "workflow.json").write_text(json.dumps({"version": 1, "projects": [first, second]}))
            for granted, allowed in (([first["project_id"]], False), ([first["project_id"], second["project_id"]], True)):
                (root / "service-auth.json").write_text(json.dumps({"identities": [{"projects": granted, "capabilities": ["workload.publish"]}]}))
                failures = module.check_configuration(root, "")
                self.assertEqual(any("workload.publish" in item for item in failures), not allowed, failures)

    def test_repository_review_requires_a_different_model_and_cost_rates(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            original = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            for reviewer in (None, original["model"], {**original["reviewer_model"], "input_micros_per_million": 0}):
                config = {**original, "reviewer_model": reviewer}
                (root / "workflow.json").write_text(json.dumps(config))
                failures = module.check_configuration(root, "")
                self.assertTrue(any("AUTO-08" in item for item in failures), failures)

    def test_native_profile_requires_the_exact_installed_engine_and_no_shared_template(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            original = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            for field in ("version", "endpoint", "namespace", "harness", "template", "revision"):
                config = json.loads(json.dumps(original))
                config["repository_engine"][field] = "changed"
                (root / "workflow.json").write_text(json.dumps(config))
                failures = module.check_configuration(root, "")
                self.assertTrue(any("native kagent" in item for item in failures), field)
    def test_retired_analysis_and_legacy_agent_installations_are_rejected(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            config["analysis"] = {"image": "analysis:latest", "harness_version": "langchain-create-agent/v1", "tools": ["read_context_source"]}
            config.pop("repository_engine", None)
            (root / "workflow.json").write_text(json.dumps(config))
            failures = module.check_configuration(root, "")
            self.assertTrue(any("retired" in item for item in failures), failures)
            self.assertTrue(any("native kagent" in item for item in failures), failures)

    def test_missing_model_and_tokens_are_reported_without_secret_values(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())
            config["model"]["name"] = ""
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
