import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[1]


class RenewalTest(unittest.TestCase):
    def test_renewal_preserves_ca_credentials_and_names(self):
        with tempfile.TemporaryDirectory() as temporary:
            installation = pathlib.Path(temporary) / "installation"
            subprocess.run(["python3", str(ROOT / "scripts/prepare-installation.py"),
                            "--local-podman", str(installation)], check=True, capture_output=True)
            preserved = {p.name: p.read_bytes() for p in installation.iterdir()
                         if p.name not in {"deck.crt", "deck.key", "marina.crt", "marina.key"}}
            original = (installation / "deck.crt").read_bytes()
            result = subprocess.run(["python3", str(ROOT / "scripts/renew-installation-certificates.py"),
                                     str(installation)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotEqual(original, (installation / "deck.crt").read_bytes())
            for name, value in preserved.items():
                self.assertEqual(value, (installation / name).read_bytes(), name)
            for service in ("deck", "marina"):
                certificate = str(installation / f"{service}.crt")
                check = subprocess.run(["openssl", "verify", "-CAfile", str(installation / "ca.crt"), certificate],
                                       capture_output=True, text=True)
                self.assertEqual(check.returncode, 0, check.stderr)
                # macOS LibreSSL has no -verify_hostname; read the names instead.
                text = subprocess.run(["openssl", "x509", "-in", certificate, "-noout", "-text"],
                                      capture_output=True, text=True, check=True).stdout
                for hostname in ("localhost", f"{service}.marina-system.svc"):
                    self.assertIn("DNS:" + hostname, text)
                self.assertEqual((installation / f"{service}.key").stat().st_mode & 0o777, 0o600)
            self.assertNotIn((installation / "operator-token").read_text().strip(), result.stdout)


if __name__ == "__main__":
    unittest.main()
