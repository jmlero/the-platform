import os
from pathlib import Path
import re
import shutil
import subprocess
import unittest

ROOT = Path(__file__).resolve().parents[1]
CHART = ROOT / "marina/deploy/helm/marina"
HELM = os.environ.get("HELM") or shutil.which("helm")


def rules(text):
    """Map each ClusterRole name to its rules text."""
    found = {}
    for document in text.split("\n---\n"):
        name = re.search(r"^kind: ClusterRole\nmetadata:(?: \{name: ([\w-]+)\}|\n  name: ([\w-]+))", document, re.MULTILINE)
        if name:
            found[name.group(1) or name.group(2)] = document[document.index("\nrules:"):].strip()
    return found


@unittest.skipUnless(HELM, "set HELM or put helm on PATH to check the Marina chart")
class MarinaChartTest(unittest.TestCase):
    def render(self, *values):
        arguments = [HELM, "template", "marina", str(CHART), "--namespace", "marina-system"]
        for value in values:
            arguments += ["--set", value]
        result = subprocess.run(arguments, capture_output=True, text=True, timeout=60)
        self.assertEqual(result.returncode, 0, result.stderr)
        return result.stdout

    def test_chart_installs_marina_without_any_client(self):
        rendered = self.render()
        self.assertNotRegex(rendered.lower(), r"deck")
        self.assertIn("- --intake-enabled=false", rendered)
        self.assertIn("name: MARINA_CONFIG_FILE", rendered)
        self.assertIn("automountServiceAccountToken: true", rendered)

    def test_deployment_keeps_the_selector_of_existing_installations(self):
        deployment = next(d for d in self.render().split("\n---\n") if "\nkind: Deployment\n" in d)
        selector = deployment[deployment.index("selector:"):deployment.index("template:")]
        self.assertEqual(sorted(re.findall(r"app\.kubernetes\.io/[\w-]+: [\w-]+", selector)),
                         ["app.kubernetes.io/name: marina", "app.kubernetes.io/part-of: the-platform"])

    def test_image_digest_and_selected_intake_are_used(self):
        digest = "sha256:" + "a" * 64
        rendered = self.render(f"image.digest={digest}", "intake.enabled=true", "intake.runID=factory-default.run-1")
        self.assertIn(f"image: ghcr.io/jmlero/the-platform-marina@{digest}", rendered)
        self.assertIn("- --intake-task-id=factory-default.run-1", rendered)

    def test_engine_and_telemetry_are_options(self):
        plain = self.render()
        self.assertNotIn("marina-engine-templates", plain)
        self.assertNotIn("OTEL_EXPORTER_OTLP_ENDPOINT", plain)
        rendered = self.render("platform.engine.enabled=true",
                               "platform.telemetry.otlpEndpoint=http://otel-collector.platform-observability.svc:4318")
        self.assertIn("name: marina-engine-templates", rendered)
        self.assertIn("name: kagent-egress-to-marina", rendered)
        self.assertIn("name: OTEL_EXPORTER_OTLP_ENDPOINT", rendered)

    def test_existing_claim_is_reused_and_not_created(self):
        rendered = self.render("persistence.existingClaim=marina-data")
        self.assertNotIn("kind: PersistentVolumeClaim", rendered)
        self.assertIn("claimName: marina-data", rendered)

    def test_roles_match_the_kustomize_api_install(self):
        kustomize = rules((ROOT / "deploy/kubernetes/platform/marina-api/rbac.yaml").read_text())
        chart = rules(self.render())
        self.assertEqual(set(chart), {"marina-platform-controller", "marina-viewer", "marina-factory", "marina-admin", "marina-capabilities"})
        self.assertEqual(set(chart), set(kustomize))
        for name, text in kustomize.items():
            self.assertEqual(chart[name].split(), text.split(), name)

    def test_crds_match_the_api_contract(self):
        for path in (ROOT / "marina/api/crds").glob("*.yaml"):
            if path.name != "kustomization.yaml":
                self.assertEqual((CHART / "crds" / path.name).read_text(), path.read_text(), path.name)


if __name__ == "__main__":
    unittest.main()
