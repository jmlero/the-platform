import json
import pathlib
import re
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


def render(profile):
    result = subprocess.run(["kubectl", "kustomize", str(ROOT / "deploy/kubernetes" / profile)],
                            capture_output=True, text=True)
    if result.returncode != 0:
        raise AssertionError(result.stderr)
    return result.stdout.split("\n---\n")


def resource(documents, kind, name):
    return next(document for document in documents
                if f"kind: {kind}\n" in document
                and re.search(rf"^  name: {re.escape(name)}$", document, re.MULTILINE))


def prepare(directory):
    output = pathlib.Path(directory) / "installation"
    result = subprocess.run(["python3", str(ROOT / "scripts/prepare-installation.py"),
                             "--local-podman", str(output)], capture_output=True, text=True)
    if result.returncode != 0:
        raise AssertionError(result.stderr)
    return output, result


class LocalPreparationTest(unittest.TestCase):
    def test_deck_is_mapped_to_a_marina_project_it_may_use_without_a_service_token(self):
        with tempfile.TemporaryDirectory() as directory:
            output, _ = prepare(directory)
            self.assertFalse((output / "service-auth.json").exists())
            self.assertFalse((output / "deck-token").exists())
            workflow = json.loads((output / "workflow.json").read_text())
            mapping = json.loads((output / "deck-marina-projects.json").read_text())
            self.assertEqual(len(mapping), 1)
            entry = mapping[0]
            self.assertEqual(entry["deck_project"], "prj_default")
            self.assertEqual(f"{entry['namespace']}.{entry['project']}", workflow["project_id"])
            factory = (output / "factory.yaml").read_text()
            self.assertIn(f"name: {entry['namespace']}", factory)
            self.assertRegex(factory, r"kind: RoleBinding[\s\S]*name: marina-factory[\s\S]*name: deck\s+namespace: marina-system")
            for workload in [*entry["workloads"].values(), entry["conversation_workload"]]:
                self.assertRegex(factory, rf"kind: Workload\s+metadata:\s+name: {workload}\b")
            self.assertNotIn("deck-token", (output / "kustomization.yaml").read_text())
            self.assertIn("deck-marina-projects.json", (output / "kustomization.yaml").read_text())

    def test_local_deck_opens_without_operator_login_and_uses_marina_with_its_kubernetes_identity(self):
        for profile in ("local-podman", "local-podman-workflow"):
            with self.subTest(profile=profile):
                documents = render(profile)
                deck = resource(documents, "Deployment", "deck")
                self.assertNotIn("DECK_OPERATOR_AUTH_FILE", deck)
                self.assertNotIn("DECK_SERVICE_AUTH_FILE", deck)
                self.assertNotIn("DECK_ARTIFACT_", deck)
                self.assertRegex(deck, r"name: DECK_MARINA_URL\s+value: https://marina.marina-system.svc:8090")
                self.assertRegex(deck, r"name: DECK_MARINA_TOKEN_FILE\s+value: /run/marina-token/token")
                self.assertIn("serviceAccountToken:", deck)
                self.assertRegex(deck, r"name: DECK_MARINA_PROJECTS_FILE\s+value: /run/deck-marina/projects.json")
                # Capabilities are cluster-scoped, so a project RoleBinding cannot grant them.
                capabilities = resource(documents, "ClusterRoleBinding", "deck-marina-capabilities")
                self.assertRegex(capabilities, r"name: marina-capabilities[\s\S]*name: deck\s+namespace: marina-system")
                marina = resource(documents, "Deployment", "marina")
                self.assertNotIn("MARINA_DECK_", marina)
                self.assertNotIn("MARINA_DECK_", resource(documents, "ConfigMap", "marina-config"))
                # Marina calls no client: nothing lets Marina reach Deck.
                self.assertNotIn("app.kubernetes.io/name: marina", resource(documents, "NetworkPolicy", "deck-ingress"))
                self.assertNotIn("app.kubernetes.io/name: deck", resource(documents, "NetworkPolicy", "marina-bootstrap-egress"))

    def test_repository_workflow_installs_the_platform_api_and_starts_without_admitting_tasks(self):
        resources = render("local-podman-workflow")
        marina = resource(resources, "Deployment", "marina")
        self.assertIn("automountServiceAccountToken: true", marina)
        self.assertRegex(marina, r"name: MARINA_CONFIG_FILE\s+value: /etc/marina/workflow.json")
        self.assertIn("- --intake-enabled=false", marina)
        worker = resource(resources, "ServiceAccount", "marina-worker")
        self.assertIn("automountServiceAccountToken: false", worker)
        self.assertTrue(any("kind: NetworkPolicy\n" in document
                            and "namespace: marina-workers\n" in document
                            for document in resources))
        resource(resources, "CustomResourceDefinition", "runs.marina.dev")
        resource(resources, "ClusterRoleBinding", "marina-platform-controller")
        resource(resources, "ClusterRole", "marina-factory")

    def test_helm_profile_leaves_marina_to_its_chart_and_keeps_deck_and_data(self):
        documents = render("local-podman-helm")
        resource(documents, "Deployment", "deck")
        resource(documents, "PersistentVolumeClaim", "marina-data")
        resource(documents, "CustomResourceDefinition", "runs.marina.dev")
        for kind, name in (("Deployment", "marina"), ("ClusterRole", "marina-platform-controller"),
                           ("Role", "marina-job-controller"), ("NetworkPolicy", "marina-gateway")):
            with self.assertRaises(StopIteration):
                resource(documents, kind, name)

    def test_local_profile_has_local_images_and_private_credentials(self):
        with tempfile.TemporaryDirectory() as directory:
            output, result = prepare(directory)
            manifest = (output / "kustomization.yaml").read_text()
            self.assertIn("deploy/kubernetes/local-podman", manifest)
            self.assertIn("localhost/the-platform/deck", manifest)
            self.assertIn("localhost/the-platform/marina", manifest)
            self.assertNotIn("YOUR_REGISTRY", manifest)
            self.assertEqual(output.stat().st_mode & 0o777, 0o700)
            token = (output / "operator-token").read_text().strip()
            self.assertEqual(len(token), 64)
            self.assertNotIn(token, result.stdout)
            self.assertEqual((output / "operator-token").stat().st_mode & 0o777, 0o600)
            for service in ("deck", "marina"):
                verified = subprocess.run(
                    ["openssl", "verify", "-x509_strict", "-CAfile", str(output / "ca.crt"),
                     str(output / f"{service}.crt")], capture_output=True, text=True)
                self.assertEqual(verified.returncode, 0, verified.stderr)


if __name__ == "__main__":
    unittest.main()
