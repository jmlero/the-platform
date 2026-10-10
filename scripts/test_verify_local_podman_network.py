import contextlib
import io
import json
from pathlib import Path
import runpy
import sys
import unittest
from unittest.mock import patch


class LocalVerifierNetworkTest(unittest.TestCase):
    def test_selected_image_runs_offline_browser_checks_in_both_network_profiles(self):
        script = Path(__file__).with_name("verify-local-podman-network.py")
        image = "localhost/the-platform/marina-verifier@sha256:" + "a" * 64
        with patch.object(sys, "argv", [str(script), "--image", image]), \
                patch("subprocess.run") as run, contextlib.redirect_stdout(io.StringIO()):
            run.return_value.returncode = 0
            runpy.run_path(str(script), run_name="__main__")
        jobs = [json.loads(call.kwargs["input"]) for call in run.call_args_list
                if "input" in call.kwargs]
        self.assertEqual([job["metadata"]["namespace"] for job in jobs],
                         ["marina-system", "marina-workers"])
        for job in jobs:
            pod = job["spec"]["template"]["spec"]
            container = pod["containers"][0]
            self.assertEqual(container["image"], image)
            self.assertFalse(pod["automountServiceAccountToken"])
            self.assertTrue(container["securityContext"]["readOnlyRootFilesystem"])
            self.assertIn("npm ci --offline", container["args"][0])
            self.assertIn("chromium.launch", container["args"][0])


if __name__ == "__main__":
    unittest.main()
