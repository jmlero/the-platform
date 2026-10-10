import json
import pathlib
import re
import subprocess
import tempfile
import unittest


class LocalPreparationTest(unittest.TestCase):
    def test_conversation_context_read_is_granted_only_for_the_bound_project(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as directory:
            output = pathlib.Path(directory) / "installation"
            result = subprocess.run(
                ["python3", str(root / "scripts/prepare-installation.py"),
                 "--local-podman", str(output)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            identity = json.loads((output / "service-auth.json").read_text())["identities"][0]
            self.assertIn("conversation.context.read", identity["capabilities"])
            self.assertEqual(identity["projects"], ["prj_default"])

    def test_local_deck_opens_without_operator_login_and_keeps_service_auth(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        for profile in ("local-podman", "local-podman-workflow"):
            with self.subTest(profile=profile):
                result = subprocess.run(
                    ["kubectl", "kustomize", str(root / "deploy/kubernetes" / profile)],
                    capture_output=True, text=True)
                self.assertEqual(result.returncode, 0, result.stderr)
                deck = next(document for document in result.stdout.split("\n---\n")
                            if "kind: Deployment\n" in document
                            and re.search(r"^  name: deck$", document, re.MULTILINE))
                self.assertNotIn("DECK_OPERATOR_AUTH_FILE", deck)
                self.assertRegex(deck, r"name: DECK_SERVICE_AUTH_FILE\s+value: /run/deck/service-auth.json")

    def test_repository_workflow_can_start_without_admitting_tasks(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        result = subprocess.run(
            ["kubectl", "kustomize", str(root / "deploy/kubernetes/local-podman-workflow")],
            capture_output=True, text=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        resources = result.stdout.split("\n---\n")
        marina = next(document for document in resources
                      if "kind: Deployment\n" in document
                      and re.search(r"^  name: marina$", document, re.MULTILINE))
        self.assertIn("automountServiceAccountToken: true", marina)
        self.assertRegex(marina, r"name: MARINA_CONFIG_FILE\s+value: /etc/marina/workflow.json")
        self.assertIn("- --intake-enabled=false", marina)
        worker = next(document for document in resources
                      if "kind: ServiceAccount\n" in document
                      and re.search(r"^  name: marina-worker$", document, re.MULTILINE))
        self.assertIn("automountServiceAccountToken: false", worker)
        self.assertTrue(any("kind: NetworkPolicy\n" in document
                            and "namespace: marina-workers\n" in document
                            for document in resources))

    def test_local_profile_has_local_images_and_private_credentials(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory() as directory:
            output = pathlib.Path(directory) / "installation"
            result = subprocess.run(
                ["python3", str(root / "scripts/prepare-installation.py"),
                 "--local-podman", str(output)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
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
