#!/usr/bin/env python3
"""Report private installation configuration blockers without exposing secrets.

Renders Kustomize locally; never applies resources or contacts Git/model providers.
A clean result is configuration preparation, not live acceptance or permission to
enable intake. Model selection and the total test budget remain operator choices.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess


def check_configuration(installation, rendered):
    config = json.loads((installation / "workflow.json").read_text())
    failures = []
    if re.search(r"YOUR_|REPLACE_", json.dumps(config)):
        failures.append("RUN-05: workflow contains unresolved placeholders")
    if re.search(r"YOUR_|REPLACE_", rendered):
        failures.append("RUN-05: rendered overlay contains unresolved placeholders")
    images = re.findall(r"^\s*image:\s*[\"']?([^\s\"']+)", rendered, re.MULTILINE)
    images.append(config.get("kubernetes", {}).get("image", ""))
    if len(images) < 3 or any(not re.fullmatch(r"[^\s]+@sha256:[0-9a-f]{64}", image) for image in images):
        failures.append("RUN-05: every control and worker image must be pinned by digest")
    for name, item in (("git-token", "RUN-03"), ("model-token", "RUN-09")):
        path = installation / name
        if not path.is_file() or not path.read_text().strip():
            failures.append(f"{item}: {name} is absent or empty")
        elif path.stat().st_mode & 0o077:
            failures.append(f"{item}: {name} must be private (chmod 600)")
    model = config.get("model", {})
    if not model.get("name") or re.search(r"YOUR_|REPLACE_|TBD", model["name"]):
        failures.append("RUN-09: exact model is not selected")
    if any(not isinstance(model.get(field), int) or model[field] <= 0 for field in
           ("input_micros_per_million", "output_micros_per_million")):
        failures.append("RUN-09/16: nonzero model rates are required for cost-cap acceptance")
    if not config.get("checks") or not config.get("allowed_paths") or not config.get("required_ci"):
        failures.append("RUN-03: configure offline checks, allowed paths and required CI")
    return failures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("installation", type=Path)
    args = parser.parse_args()
    installation = args.installation.expanduser().resolve(strict=True)
    try:
        rendered = subprocess.run(["kubectl", "kustomize", str(installation)], check=True,
                                  capture_output=True, text=True, timeout=30).stdout
        failures = check_configuration(installation, rendered)
        for service in ("deck", "marina"):
            subprocess.run(["openssl", "verify", "-x509_strict", "-CAfile", str(installation / "ca.crt"),
                            str(installation / f"{service}.crt")], check=True, capture_output=True, timeout=10)
    except (OSError, ValueError, subprocess.SubprocessError):
        parser.exit(1, "Installation preflight failed to read/render configuration or verify TLS; private content omitted.\n")
    print(json.dumps({"configuration_ready": not failures, "blockers": failures,
                      "live_acceptance": "not performed", "intake_changed": False}, indent=2))
    return 1 if failures else 0


if __name__ == "__main__":
    raise SystemExit(main())
