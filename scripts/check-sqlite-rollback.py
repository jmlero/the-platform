#!/usr/bin/env python3
"""Check copied SQLite stores against a retained release's schema inventory.

Use the manifest recorded while that release was installed. This read-only
gate does not downgrade data or deploy images. Restore its compatible snapshot
before starting an older binary when this check rejects the current stores.
"""
import argparse
import json
from pathlib import Path
import re
import sqlite3


MIGRATION = re.compile(r"^[0-9]{3}_[a-z0-9_]+\.sql$")
IMAGE = re.compile(r"^[^\s]+@sha256:[0-9a-f]{64}$")


def check(manifest, databases):
    results = {}
    for product, path in databases.items():
        supported = manifest["databases"][product]["schema_migrations"]
        image = manifest["images"][product]
        if (not isinstance(supported, list) or not supported or
                any(not isinstance(v, str) or not MIGRATION.fullmatch(v) for v in supported) or
                len(set(supported)) != len(supported) or not isinstance(image, str) or not IMAGE.fullmatch(image)):
            raise ValueError("invalid retained release inventory")
        with sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True) as db:
            db.execute("BEGIN")
            integrity = [row[0] for row in db.execute("PRAGMA integrity_check")]
            applied = [row[0] for row in db.execute("SELECT version FROM schema_migrations ORDER BY version")]
        unsupported = sorted(set(applied) - set(supported))
        results[product] = {
            "image": image, "integrity_ok": integrity == ["ok"],
            "applied_migrations": applied, "unsupported_migrations": unsupported,
            "compatible": bool(applied) and integrity == ["ok"] and not unsupported,
        }
    return {"compatible": all(v["compatible"] for v in results.values()), "databases": results}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("release_manifest", type=Path)
    parser.add_argument("--deck-db", type=Path, required=True)
    parser.add_argument("--marina-db", type=Path, required=True)
    args = parser.parse_args()
    try:
        result = check(json.loads(args.release_manifest.read_text()), {"deck": args.deck_db, "marina": args.marina_db})
    except (OSError, ValueError, KeyError, TypeError, sqlite3.Error):
        print(json.dumps({"compatible": False, "reason": "release inventory or copied database could not be validated"}))
        return 1
    if not result["compatible"]:
        result["required_action"] = "Restore the retained compatible snapshot before starting the previous release."
    print(json.dumps(result, indent=2))
    return 0 if result["compatible"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
