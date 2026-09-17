import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("egress", Path(__file__).with_name("prepare-local-podman-egress.py"))
egress = importlib.util.module_from_spec(spec)
spec.loader.exec_module(egress)


class LocalEgressTest(unittest.TestCase):
    def test_controller_uses_the_same_provider_addresses_as_the_policy(self):
        resolved = {host: ["140.82.121.4", "140.82.121.3"] for host in egress.HOSTS}
        patch = egress.build_deployment_patch(resolved)
        aliases = patch["spec"]["template"]["spec"]["hostAliases"]
        self.assertEqual({host for item in aliases for host in item["hostnames"]}, set(egress.HOSTS))
        for item in aliases:
            for host in item["hostnames"]:
                self.assertIn(item["ip"], resolved[host])
        self.assertEqual(patch["metadata"]["name"], "marina")

    def test_allows_only_resolved_provider_addresses_and_exact_api_ports(self):
        resolved = {host: ["140.82.121.4"] for host in egress.HOSTS}
        policy = egress.build_policy(resolved, "10.96.0.1", [("10.89.0.2", 6443)])
        rules = policy["spec"]["egress"]
        self.assertEqual(rules[0], {"to": [{"ipBlock": {"cidr": "140.82.121.4/32"}}],
                                   "ports": [{"protocol": "TCP", "port": 443}]})
        self.assertEqual(rules[1]["to"], [{"ipBlock": {"cidr": "10.96.0.1/32"}}])
        self.assertEqual(rules[1]["ports"][0]["port"], 443)
        self.assertEqual(rules[2]["ports"][0]["port"], 6443)
        self.assertEqual(policy["spec"]["podSelector"]["matchLabels"], {"app.kubernetes.io/name": "marina"})

    def test_missing_or_private_provider_resolution_fails_closed(self):
        for bad in ([], ["127.0.0.1"], ["10.0.0.1"], ["0.0.0.0/0"]):
            with self.subTest(addresses=bad), self.assertRaises(ValueError):
                resolved = {host: ["140.82.121.4"] for host in egress.HOSTS}
                resolved["openrouter.ai"] = bad
                egress.build_policy(resolved, "10.96.0.1", [("10.89.0.2", 6443)])

    def test_missing_api_endpoints_fail_closed(self):
        with self.assertRaises(ValueError):
            egress.build_policy({host: ["140.82.121.4"] for host in egress.HOSTS}, "10.96.0.1", [])
