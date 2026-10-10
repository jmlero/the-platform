import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class LocalStartupTest(unittest.TestCase):
    def test_existing_native_harness_registry_starts_after_the_kind_node(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            stub = directory / "podman"
            stub.write_text("""#!/usr/bin/env python3
import json, os, sys
with open(os.environ['STARTUP_TEST_LOG'], 'a') as log:
    log.write(json.dumps(sys.argv[1:]) + '\\n')
if sys.argv[1:3] == ['machine', 'inspect']: print('running')
""")
            stub.chmod(0o700)
            log = directory / "calls.jsonl"
            env = {**os.environ, "PATH": str(directory) + os.pathsep + os.environ["PATH"],
                   "STARTUP_TEST_LOG": str(log)}
            subprocess.run(["/bin/sh", str(Path(__file__).with_name("local-podman-start.sh"))], env=env, check=True, capture_output=True)
            calls = [json.loads(line) for line in log.read_text().splitlines()]
            starts = [call for call in calls if "start" in call]
            self.assertEqual(starts, [
                ["--connection", "podman-machine-default-root", "start", "marina-local-control-plane"],
                ["--connection", "podman-machine-default-root", "start", "kint-local-registry"],
            ])


if __name__ == "__main__":
    unittest.main()
