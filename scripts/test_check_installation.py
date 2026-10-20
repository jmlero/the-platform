import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location("check_installation", Path(__file__).with_name("check-installation.py"))


def example():
    return json.loads((Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/workflow.example.json").read_text())


def mapping(namespace):
    return {"deck_project": "prj_" + namespace, "namespace": namespace, "project": "acceptance",
            "workloads": {"repository/v2": "land-on-main"}, "conversation_workload": "chat"}


def binding(namespace):
    return f"""apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: deck-marina-factory
  namespace: {namespace}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: marina-factory
subjects:
- kind: ServiceAccount
  name: deck
  namespace: marina-system"""


class InstallationPreflightTest(unittest.TestCase):

    def test_each_configured_project_must_be_mapped_for_deck_with_access(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = {**example(), "delivery": "fast_forward"}
            second = json.loads(json.dumps(first))
            second["project_id"] = "factory-second.acceptance"
            (root / "workflow.json").write_text(json.dumps({"version": 1, "projects": [first, second]}))
            entries = [mapping("factory-default"), mapping("factory-second")]
            for mapped, bound, allowed in ((entries[:1], ["factory-default"], False),
                                           (entries, ["factory-default"], False),
                                           (entries, ["factory-default", "factory-second"], True)):
                with self.subTest(mapped=len(mapped), bound=bound):
                    (root / "deck-marina-projects.json").write_text(json.dumps(mapped))
                    failures = module.check_configuration(root, "\n---\n".join(binding(namespace) for namespace in bound))
                    self.assertEqual(any("MAR-10" in item for item in failures), not allowed, failures)

    def test_project_id_must_name_a_marina_namespace_and_project(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = example()
            config["project_id"] = "prj_default"
            (root / "workflow.json").write_text(json.dumps(config))
            failures = module.check_configuration(root, "")
            self.assertTrue(any("MAR-10: project_id" in item for item in failures), failures)

    def test_chat_profile_requires_a_mapped_conversation_workload(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "workflow.json").write_text(json.dumps(example()))
            entry = mapping("factory-default")
            del entry["conversation_workload"]
            (root / "deck-marina-projects.json").write_text(json.dumps([entry]))
            failures = module.check_configuration(root, binding("factory-default"))
            self.assertTrue(any("conversation" in item and "MAR-10" in item for item in failures), failures)

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

    def test_marina_installed_by_its_chart_counts_with_its_pinned_image(self):
        module = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(module)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            config = example()
            config["kubernetes"]["image"] = "registry/verifier@sha256:" + "a" * 64
            (root / "workflow.json").write_text(json.dumps(config))
            rendered = "image: registry/deck@sha256:" + "b" * 64 + "\n"
            for values, pinned in (("image:\n  repository: registry/marina\n  tag: latest\n", False),
                                   ("image:\n  repository: registry/marina\n  digest: sha256:" + "c" * 64 + "\n", True)):
                with self.subTest(pinned=pinned):
                    (root / "marina-values.yaml").write_text(values)
                    failures = module.check_configuration(root, rendered)
                    self.assertEqual(any("digest" in item for item in failures), not pinned, failures)

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
