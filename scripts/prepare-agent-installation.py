#!/usr/bin/env python3
"""Prepare the bounded two-workload profile in an existing private installation.

Offline only: does not deploy, change intake, rotate credentials or touch stores.
Drain existing work before applying an installation upgrade. Preview is default.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def prepare(config, auth, image, identity):
    if not re.fullmatch(r"[^\s]+@sha256:[0-9a-f]{64}", image):
        raise ValueError("analysis image requires an immutable SHA-256 manifest digest")
    config, auth = copy.deepcopy(config), copy.deepcopy(auth)
    if config.get("version", 1) != 1:
        raise ValueError("unsupported workflow version")
    matches = [item for item in auth["identities"] if item["id"] == identity
               and config["project_id"] in item["projects"]]
    if len(matches) != 1:
        raise ValueError("select exactly one existing service identity scoped to this project")
    old = config.get("analysis")
    profile = copy.deepcopy(old) if old else {
        "version": 1, "revision": 1, "max_tokens": min(20000, config["max_tokens"]),
        "budget_minutes": min(5, config["budget_minutes"]),
        "fixture": json.loads((ROOT / "marina/internal/analysis/fixtures/blocked-runs-v1.json").read_text()),
    }
    profile.update(image=image, harness_version="langchain-create-agent/v2", tools=["read_context_source"])
    if old and profile != old:
        profile["revision"] += 1
    config.update(version=1, analysis=profile)
    for capability in ("context.read", "repository.read", "repository.write", "workload.publish"):
        if capability not in matches[0]["capabilities"]:
            matches[0]["capabilities"].append(capability)
    return config, auth


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("installation", type=Path)
    parser.add_argument("--analysis-image", required=True)
    parser.add_argument("--identity", default="marina")
    parser.add_argument("--marina-binary", type=Path, default=ROOT / "marina/bin/marina")
    parser.add_argument("--apply", action="store_true", help="save validated configuration with private backups; never deploy")
    args = parser.parse_args()
    os.umask(0o077)
    installation = args.installation.expanduser().resolve(strict=True)
    if installation == ROOT or ROOT in installation.parents:
        parser.error("the installation must be outside the repository")
    try:
        paths = [installation / name for name in ("workflow.json", "service-auth.json")]
        previous = [path.read_bytes() for path in paths]
        prepared = prepare(*(json.loads(value) for value in previous), args.analysis_image, args.identity)
        encoded = [(json.dumps(value, indent=2) + "\n").encode() for value in prepared]
        with tempfile.TemporaryDirectory(prefix="agent-profile-") as temporary:
            candidate = Path(temporary) / "workflow.json"
            candidate.write_bytes(encoded[0])
            result = subprocess.run([str(args.marina_binary.resolve()), "workload-profile", "--config", str(candidate)],
                                    check=True, capture_output=True, text=True, timeout=15)
            profile = json.loads(result.stdout)
        if args.apply and encoded != previous:
            # Originals remain recoverable if a write is interrupted. Never overwrite them.
            backup = installation / ("agent-upgrade-" + hashlib.sha256(b"".join(previous)).hexdigest()[:16])
            backup.mkdir(mode=0o700, exist_ok=True)
            for path, before, after in zip(paths, previous, encoded):
                saved = backup / path.name
                if not saved.exists():
                    saved.write_bytes(before)
                staged = path.with_suffix(path.suffix + ".next")
                staged.write_bytes(after)
                staged.chmod(0o600)
                staged.replace(path)
        print(json.dumps({"applied": args.apply, "profile": profile, "intake_changed": False,
                          "credentials_rotated": False, "cluster_contacted": False}, indent=2))
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        parser.exit(1, "Agent preparation failed; check profile, image digest, service identity and Marina binary. Private configuration omitted.\n")


if __name__ == "__main__":
    main()
