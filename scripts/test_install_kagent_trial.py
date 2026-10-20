import importlib.util
from pathlib import Path
import unittest


class TrialInstallTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location(
            "install_trial", Path(__file__).with_name("install-kagent-trial.py"))
        cls.install = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.install)

    def test_existing_namespace_prevents_installation(self):
        obj = {"kind": "Namespace", "metadata": {"name": "ate-system"}}
        with self.assertRaisesRegex(ValueError, "collision"):
            self.install.check_collisions([obj], [obj])

    def test_existing_cluster_resource_prevents_installation(self):
        obj = {"kind": "ClusterRole", "metadata": {"name": "ate-controller"}}
        with self.assertRaisesRegex(ValueError, "collision"):
            self.install.check_collisions([obj], [obj])

    def test_unrelated_platform_resources_are_preserved(self):
        self.install.check_collisions(
            [{"kind": "Namespace", "metadata": {"name": "ate-system"}}],
            [{"kind": "Namespace", "metadata": {"name": "marina-system"}}])

    def test_permanent_install_relabels_owned_resources_and_policy_selectors(self):
        resources = [{"kind": "Namespace", "metadata": {"name": "kint-engine",
                      "labels": {"the-platform.dev/trial": "kint-02"}}}]
        policies = [{"kind": "NetworkPolicy", "metadata": {"name": "internal",
                     "namespace": "ate-system", "labels": {"the-platform.dev/trial": "kint-02"}},
                     "spec": {"ingress": [{"from": [{"namespaceSelector": {
                         "matchLabels": {"the-platform.dev/trial": "kint-02"}}}]}]}}]
        self.install.prepare_permanent(resources, policies)
        self.assertEqual(resources[0]["metadata"]["labels"],
                         {"the-platform.dev/installation": "local-kagent"})
        self.assertEqual(policies[0]["metadata"]["labels"],
                         {"the-platform.dev/installation": "local-kagent"})
        self.assertEqual(policies[0]["spec"]["ingress"][0]["from"][0]
                         ["namespaceSelector"]["matchLabels"],
                         {"the-platform.dev/installation": "local-kagent"})

    def test_policies_follow_the_selected_node_address(self):
        policies = [{"spec": {"egress": [{"to": [{"ipBlock": {"cidr": "10.89.0.2/32"}}]}]}}]
        self.install.pin_node(policies, "10.89.1.2")
        self.assertEqual(policies[0]["spec"]["egress"][0]["to"][0]["ipBlock"]["cidr"],
                         "10.89.1.2/32")


if __name__ == "__main__":
    unittest.main()
