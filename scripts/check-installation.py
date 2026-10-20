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
    profiles = config.get("projects")
    if profiles is None:
        return check_profile(config, installation, rendered)
    if config.get("version") != 1 or set(config) != {"version", "projects"} or not isinstance(profiles, list) or not 1 <= len(profiles) <= 16 or any(not isinstance(profile, dict) for profile in profiles):
        return ["PROJ-01: bounded version-1 project profiles are required"]
    failures = []
    seen = set()
    shared = profiles[0]
    for profile in profiles:
        project = profile.get("project_id")
        if not isinstance(project, str) or not project or project in seen:
            failures.append("PROJ-01: each project requires one explicit profile")
        if isinstance(project, str):
            seen.add(project)
        if any(profile.get(field) != shared.get(field) for field in ("public_url", "tls_cert_file", "tls_key_file", "signing_key_file", "artifact_directory", "artifact_token_file")):
            failures.append("PROJ-01: project profiles must share the service and artifact installation")
        failures.extend(check_profile(profile, installation, rendered))
    return sorted(set(failures))


def check_deck_mapping(config, installation, rendered):
    """Deck reaches each Marina project through its mapping and RoleBinding (decision 0033)."""
    project = config.get("project_id")
    namespace, _, name = project.partition(".") if isinstance(project, str) else ("", "", "")
    label = r"[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?"
    if not re.fullmatch(label, namespace) or not re.fullmatch(label, name):
        return ["MAR-10: project_id must be <namespace>.<project> of the Marina Project"]
    try:
        entries = json.loads((installation / "deck-marina-projects.json").read_text())
        entry = next(item for item in entries if item.get("namespace") == namespace and item.get("project") == name)
    except (OSError, ValueError, AttributeError, TypeError, StopIteration):
        return [f"MAR-10: map a Deck project to Marina project {project} in deck-marina-projects.json"]
    failures = []
    profile = "repository/v2" if config.get("delivery") == "fast_forward" else "repository/v1"
    if config.get("git", {}).get("repository") and not (entry.get("workloads") or {}).get(profile):
        failures.append(f"MAR-10: map {profile} to a Workload for {project}")
    if config.get("conversation") and not entry.get("conversation_workload"):
        failures.append(f"MAR-10: map a conversation Workload for {project}")
    bound = any("kind: RoleBinding\n" in document and f"namespace: {namespace}\n" in document
                and re.search(r"^  name: marina-factory$", document, re.MULTILINE)
                and re.search(r"^  name: deck\n  namespace: marina-system$", document, re.MULTILINE)
                for document in rendered.split("\n---\n"))
    if not bound:
        failures.append(f"MAR-10: bind Deck's service account to marina-factory in namespace {namespace}")
    return failures


def check_profile(config, installation, rendered):
    failures = []
    if re.search(r"YOUR_|REPLACE_", json.dumps(config)):
        failures.append("RUN-05: workflow contains unresolved placeholders")
    if re.search(r"YOUR_|REPLACE_", rendered):
        failures.append("RUN-05: rendered overlay contains unresolved placeholders")
    images = re.findall(r"^\s*image:\s*[\"']?([^\s\"']+)", rendered, re.MULTILINE)
    images.append(config.get("kubernetes", {}).get("image", ""))
    chart = installation / "marina-values.yaml"
    if chart.is_file():
        # Marina installed by its Helm chart (MAR-11): its image is in the values.
        values = chart.read_text()
        repository = re.search(r"^  repository:\s*(\S+)", values, re.MULTILINE)
        digest = re.search(r"^  digest:\s*(\S+)", values, re.MULTILINE)
        images.append(f"{repository.group(1) if repository else ''}@{digest.group(1) if digest else ''}")
    if len(images) < 3 or any(not re.fullmatch(r"[^\s]+@sha256:[0-9a-f]{64}", image) for image in images):
        failures.append("RUN-05: every control and worker image must be pinned by digest")
    if "analysis" in config or "analysis_engine" in config:
        failures.append("KINT-14: diagnostic analysis configuration is retired; prepare the native installation upgrade")
    if config.get("git", {}).get("repository"):
        engine = config.get("repository_engine") or {}
        reference = json.loads((Path(__file__).resolve().parents[1] /
                                "deploy/kubernetes/first-release/workflow.example.json").read_text())["repository_engine"]
        if (any(engine.get(field) != value for field, value in reference.items()) or
                engine.get("template") or engine.get("revision") or
                config.get("repository_engine_gateway_url") != "https://marina.marina-system.svc"):
            failures.append("KINT-14: repository work requires the installed native kagent profile and gateway")
    failures.extend(check_deck_mapping(config, installation, rendered))
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
    if config.get("git", {}).get("repository"):
        reviewer = config.get("reviewer_model") or {}
        name = reviewer.get("name", "")
        if (not isinstance(name, str) or not name.strip() or re.search(r"YOUR_|REPLACE_|TBD", name)
                or name.strip().casefold() == str(model.get("name", "")).strip().casefold()):
            failures.append("AUTO-08: select a different explicit reviewer model")
        if (reviewer.get("currency") != model.get("currency") or any(
                not isinstance(reviewer.get(field), int) or reviewer[field] <= 0
                for field in ("input_micros_per_million", "output_micros_per_million"))):
            failures.append("AUTO-08: configure nonzero reviewer rates in the task currency")
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
