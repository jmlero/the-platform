"""The disposable fixture must fail for its known defect and pass after repair."""
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class AcceptanceFixtureTest(unittest.TestCase):
    def test_fixture_detects_upper_boundary_defect_and_accepts_repair(self):
        source = Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/acceptance-repository"
        with tempfile.TemporaryDirectory() as directory:
            checkout = Path(directory) / "fixture"
            shutil.copytree(source, checkout)
            command = ["node", "--test", "tests/clamp.test.cjs"]
            broken = subprocess.run(command, cwd=checkout, capture_output=True, text=True)
            self.assertNotEqual(broken.returncode, 0)
            self.assertIn("upper bound is inclusive", broken.stdout)
            implementation = checkout / "src/clamp.cjs"
            implementation.write_text(implementation.read_text().replace("maximum - 1", "maximum"))
            repaired = subprocess.run(command, cwd=checkout, capture_output=True, text=True)
            self.assertEqual(repaired.returncode, 0, repaired.stdout + repaired.stderr)


if __name__ == "__main__":
    unittest.main()
