import importlib.util
from pathlib import Path
import unittest


class LocalChatTurnTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        spec = importlib.util.spec_from_file_location(
            "prepare_chat", Path(__file__).with_name("prepare-local-kagent-chat-turn.py"))
        cls.prepare = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(cls.prepare)

    def test_new_turn_uses_its_own_secret_and_model_config(self):
        template = {"apiVersion": "kagent.dev/v1alpha3", "kind": "AgentTemplate",
                    "metadata": {"name": "turn-1", "labels": {"kagent.dev/harness": "chat"}},
                    "spec": {"modelConfig": {"name": "model-1"}, "systemPrompt": "read only"}}
        model = {"apiVersion": "kagent.dev/v1alpha3", "kind": "ModelConfig",
                 "spec": {"apiKeySecret": "turn-1", "apiKeySecretKey": "gateway", "model": "conversation"}}
        resources = self.prepare.build_resources(template, model, "turn-2", "placeholder")
        secret, new_model, new_template = resources
        self.assertEqual(secret["metadata"]["name"], "turn-2")
        self.assertEqual(new_model["metadata"]["name"], "turn-2-model")
        self.assertEqual(new_model["spec"]["apiKeySecret"], "turn-2")
        self.assertEqual(new_template["spec"]["modelConfig"]["name"], "turn-2-model")
        self.assertEqual(template["spec"]["modelConfig"]["name"], "model-1")
        self.assertEqual(model["spec"]["apiKeySecret"], "turn-1")

    def test_private_selection_changes_only_chat_and_exact_secret_grant(self):
        workflow = {"repository_engine": {"template": "legacy"},
                    "conversation": {"engine": {"template": "turn-1", "revision": "a" * 64}}}
        overlay = "patches:\n  - target: {kind: Role, name: marina-conversation-credential, namespace: kint-engine}\n    patch: |-\n      - op: replace\n        path: /rules/0/resourceNames\n        value: [turn-1]\n"
        updated, patch = self.prepare.select_turn(workflow, overlay, "turn-2", "b" * 64)
        self.assertEqual(updated["conversation"]["engine"], {"template": "turn-2", "revision": "b" * 64})
        self.assertEqual(updated["repository_engine"], {"template": "legacy"})
        self.assertIn("value: [turn-2]", patch)
        self.assertNotIn("value: [turn-1]", patch)


if __name__ == "__main__":
    unittest.main()
