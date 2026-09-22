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


if __name__ == "__main__":
    unittest.main()
