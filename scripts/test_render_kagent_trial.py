import importlib.util
from pathlib import Path
import unittest


class TrialRenderTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location(
            "trial", Path(__file__).with_name("render-kagent-trial.py"))
        cls.trial = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.trial)

    def test_unlocked_image_is_rejected_before_installation(self):
        pod = {"kind": "Pod", "metadata": {"name": "test"},
               "spec": {"containers": [{"name": "test", "image": "unknown:latest"}]}}
        with self.assertRaisesRegex(ValueError, "unlocked image"):
            self.trial.pin_images(pod, {})

    def test_cluster_secret_access_is_scoped_to_trial_namespaces(self):
        role = {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole",
                "metadata": {"name": "ate-controller"}, "rules": [
                    {"apiGroups": [""], "resources": ["pods", "secrets"],
                     "verbs": ["get", "list", "watch"]}]}
        scoped = self.trial.scope_secrets(role)
        self.assertEqual(role["rules"][0]["resources"], ["pods"])
        self.assertEqual({x["metadata"]["namespace"] for x in scoped}, {"ate-system"})
        self.assertEqual(scoped[0]["rules"][0]["resources"], ["secrets"])

    def test_certificate_v1_does_not_satisfy_beta_client_requirement(self):
        self.assertFalse(self.trial.certificate_api_ready({"versions": [
            {"groupVersion": "certificates.k8s.io/v1"}]}))
        self.assertTrue(self.trial.certificate_api_ready({"versions": [
            {"groupVersion": "certificates.k8s.io/v1beta1"}]}))

    def test_controller_cannot_change_other_products_deployments(self):
        role = {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "ClusterRole",
                "metadata": {"name": "ate-controller"}, "rules": [
                    {"apiGroups": ["apps"], "resources": ["deployments"],
                     "verbs": ["get", "list", "watch", "create", "delete", "patch", "update"]}]}
        scoped = self.trial.scope_mutations(role)
        self.assertEqual(role["rules"][0]["verbs"], ["get", "list", "watch"])
        self.assertEqual(scoped[0]["metadata"]["namespace"], "kint-engine")


if __name__ == "__main__":
    unittest.main()
