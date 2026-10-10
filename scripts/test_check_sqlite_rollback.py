import json
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("check-sqlite-rollback.py")


class SQLiteRollbackTests(unittest.TestCase):
    def test_rejects_newer_schema_without_changing_either_database(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = {}
            versions = {"deck": ["001_initial.sql"], "marina": ["001_initial.sql"]}
            for product in versions:
                path = root / (product + ".db")
                with sqlite3.connect(path) as db:
                    db.execute("CREATE TABLE schema_migrations (version TEXT PRIMARY KEY)")
                    db.execute("INSERT INTO schema_migrations VALUES ('001_initial.sql')")
                    db.execute("CREATE TABLE retained_history (body TEXT)")
                    db.execute("INSERT INTO retained_history VALUES ('preserve this record')")
                paths[product] = path
            with sqlite3.connect(paths["deck"]) as db:
                db.execute("INSERT INTO schema_migrations VALUES ('002_new_schema.sql')")
            manifest = root / "release.json"
            manifest.write_text(json.dumps({
                "images": {p: "localhost/the-platform/" + p + "@sha256:" + "a" * 64 for p in versions},
                "databases": {p: {"schema_migrations": v} for p, v in versions.items()},
            }))
            before = {p: path.read_bytes() for p, path in paths.items()}
            command = ["python3", str(SCRIPT), str(manifest), "--deck-db", str(paths["deck"]), "--marina-db", str(paths["marina"])]
            rejected = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(rejected.returncode, 1, rejected.stderr)
            result = json.loads(rejected.stdout)
            self.assertFalse(result["compatible"])
            self.assertEqual(result["databases"]["deck"]["unsupported_migrations"], ["002_new_schema.sql"])
            self.assertEqual(before, {p: path.read_bytes() for p, path in paths.items()})
            versions["deck"].append("002_new_schema.sql")
            data = json.loads(manifest.read_text())
            data["databases"]["deck"]["schema_migrations"] = versions["deck"]
            manifest.write_text(json.dumps(data))
            accepted = subprocess.run(command, capture_output=True, text=True)
            self.assertEqual(accepted.returncode, 0, accepted.stderr)
            self.assertTrue(json.loads(accepted.stdout)["compatible"])
            self.assertEqual(before, {p: path.read_bytes() for p, path in paths.items()})


if __name__ == "__main__":
    unittest.main()
