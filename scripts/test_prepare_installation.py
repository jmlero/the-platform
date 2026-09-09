import pathlib
import subprocess
import tempfile
import unittest


class LocalPreparationTest(unittest.TestCase):
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
            self.assertIn("localhost/the-platform/appdeck", manifest)
            self.assertIn("localhost/the-platform/marina", manifest)
            self.assertNotIn("YOUR_REGISTRY", manifest)
            self.assertEqual(output.stat().st_mode & 0o777, 0o700)
            token = (output / "operator-token").read_text().strip()
            self.assertEqual(len(token), 64)
            self.assertNotIn(token, result.stdout)
            self.assertEqual((output / "operator-token").stat().st_mode & 0o777, 0o600)
            for service in ("appdeck", "marina"):
                verified = subprocess.run(
                    ["openssl", "verify", "-x509_strict", "-CAfile", str(output / "ca.crt"),
                     str(output / f"{service}.crt")], capture_output=True, text=True)
                self.assertEqual(verified.returncode, 0, verified.stderr)


if __name__ == "__main__":
    unittest.main()
