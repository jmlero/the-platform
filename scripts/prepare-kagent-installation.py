#!/usr/bin/env python3
"""Prepare removal of retired agent configuration after the native cutover.

Preview by default. Never deploys, enables intake, contacts providers, changes
grants, rotates credentials or touches stores. Drain work before applying.
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


def prepare(config, image):
    if not re.fullmatch(r"[^\s]+@sha256:[0-9a-f]{64}", image):
        raise ValueError("verifier image requires an immutable SHA-256 manifest digest")
    if config.get("version", 1) != 1 or not config.get("repository_engine"):
        raise ValueError("complete the native engine cutover before removing legacy configuration")
    prepared = copy.deepcopy(config)
    prepared.pop("analysis", None)
    prepared.pop("analysis_engine", None)
    prepared["kubernetes"]["image"] = image
    return prepared


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("installation", type=Path)
    parser.add_argument("--verifier-image", required=True)
    parser.add_argument("--marina-binary", type=Path, default=ROOT / "marina/bin/marina")
    parser.add_argument("--apply", action="store_true", help="save validated configuration with a private original; never deploy")
    args = parser.parse_args()
    os.umask(0o077)
    installation = args.installation.expanduser().resolve(strict=True)
    if installation == ROOT or ROOT in installation.parents:
        parser.error("the installation must be outside the repository")
    try:
        path = installation / "workflow.json"
        previous = path.read_bytes()
        prepared = prepare(json.loads(previous), args.verifier_image)
        encoded = (json.dumps(prepared, indent=2) + "\n").encode()
        with tempfile.TemporaryDirectory(prefix="kagent-profile-") as temporary:
            candidate = Path(temporary) / "workflow.json"
            candidate.write_bytes(encoded)
            result = subprocess.run([str(args.marina_binary.resolve()), "check-config", "--config", str(candidate)],
                                    check=True, capture_output=True, text=True, timeout=15)
            validated = json.loads(result.stdout)
        if args.apply and encoded != previous:
            backup = installation / ("kagent-removal-" + hashlib.sha256(previous).hexdigest()[:16])
            backup.mkdir(mode=0o700, exist_ok=True)
            saved = backup / path.name
            if not saved.exists():
                saved.write_bytes(previous)
            staged = path.with_suffix(path.suffix + ".next")
            staged.write_bytes(encoded)
            staged.chmod(0o600)
            staged.replace(path)
        print(json.dumps({"applied": args.apply, **validated, "intake_changed": False,
                          "credentials_rotated": False, "cluster_contacted": False}, indent=2))
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError):
        parser.exit(1, "Native upgrade preparation failed; check the selected engine, verifier digest and Marina binary. Private configuration omitted.\n")


if __name__ == "__main__":
    main()
