from pathlib import Path
import re
import unittest

PLATFORM = Path(__file__).resolve().parents[1] / "deploy/kubernetes/platform"


class PlatformManifestTest(unittest.TestCase):

    def test_every_platform_image_is_pinned_by_digest(self):
        references = []
        for path in PLATFORM.rglob("*.yaml"):
            for number, line in enumerate(path.read_text().splitlines(), 1):
                match = re.match(r"\s*(?:-\s*)?(image|tag):\s*[\"']?([^\"'\s#]+)", line)
                if match and match.group(2) not in ("{}", "|-", ">-"):
                    references.append((path.relative_to(PLATFORM), number, match.group(2)))
        self.assertTrue(references, "no image references found")
        for path, number, value in references:
            with self.subTest(path=str(path), line=number):
                self.assertRegex(value, r"@sha256:[0-9a-f]{64}$")

    def test_kyverno_denies_only_in_platform_owned_namespaces(self):
        policies = PLATFORM / "kyverno/policies"
        enforce = (policies / "enforce/kustomization.yaml").read_text()
        audit = (policies / "audit/kustomization.yaml").read_text()
        self.assertIn("value: [Deny]", enforce)
        self.assertIn("values: [marina-system, marina-workers, platform-gateway, platform-ingress, platform-observability]", enforce)
        self.assertIn("value: [Audit]", audit)
        self.assertNotIn("marina-system", audit)


if __name__ == "__main__":
    unittest.main()
