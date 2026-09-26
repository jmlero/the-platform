#!/usr/bin/env python3
"""Prepare one fresh, attempt-exclusive chat turn in kind-marina-local.

Run only after the previous turn has finished. The existing Harness, model
policy, and private workflow stay authoritative; this rotates only the prepared
template, model credential Secret, and exact Secret grant.
"""
import argparse
import base64
import copy
import json
from pathlib import Path
import re
import secrets
import subprocess
import time


KUBE = ["kubectl", "--context", "kind-marina-local", "-n", "kint-engine"]
ROLE_PATCH = "name: marina-conversation-credential, namespace: kint-engine"


def build_resources(template, model, name, placeholder):
    if not re.fullmatch(r"[a-z0-9]([-a-z0-9]{0,48}[a-z0-9])?", name):
        raise ValueError("template name must be a short Kubernetes DNS name")
    if name == template["metadata"]["name"] or not placeholder:
        raise ValueError("a fresh template name and Secret placeholder are required")
    new_model_name = name + "-model"
    new_model = {"apiVersion": model["apiVersion"], "kind": "ModelConfig",
                 "metadata": {"name": new_model_name, "namespace": "kint-engine",
                              "labels": {"the-platform.dev/installation": "local-kagent"}},
                 "spec": copy.deepcopy(model["spec"])}
    new_model["spec"]["apiKeySecret"] = name
    new_template = {"apiVersion": template["apiVersion"], "kind": "AgentTemplate",
                    "metadata": {"name": name, "namespace": "kint-engine",
                                 "labels": copy.deepcopy(template["metadata"]["labels"])},
                    "spec": copy.deepcopy(template["spec"])}
    new_template["spec"]["modelConfig"]["name"] = new_model_name
    secret = {"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
              "metadata": {"name": name, "namespace": "kint-engine",
                           "labels": {"the-platform.dev/installation": "local-kagent"}},
              "data": {"gateway": base64.b64encode(placeholder.encode()).decode()}}
    return secret, new_model, new_template


def select_turn(workflow, overlay, name, revision):
    if not re.fullmatch(r"[0-9a-f]{64}", revision):
        raise ValueError("ready template revision must be a SHA-256 digest")
    result = copy.deepcopy(workflow)
    old_name = result["conversation"]["engine"]["template"]
    if old_name == name or overlay.count(ROLE_PATCH) != 1:
        raise ValueError("private chat profile and exact RBAC patch are required")
    old_grant = "value: [" + old_name + "]"
    if overlay.count(old_grant) != 1:
        raise ValueError("private RBAC grant does not match selected template")
    result["conversation"]["engine"]["template"] = name
    result["conversation"]["engine"]["revision"] = revision
    return result, overlay.replace(old_grant, "value: [" + name + "]")


def kube(*args, document=None):
    result = subprocess.run(KUBE + list(args), input=None if document is None else json.dumps(document),
                            capture_output=True, text=True, timeout=60)
    if result.returncode:
        raise RuntimeError("kubectl " + " ".join(args[:3]) + " failed; inspect the dedicated local context")
    return result.stdout


def get(kind, name):
    return json.loads(kube("get", kind, name, "-o", "json"))


def wait_revision(name):
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        item = get("agenttemplate", name)
        for harness in item.get("status", {}).get("harnesses", []):
            if any(c.get("type") == "Ready" and c.get("status") == "True" for c in harness.get("conditions", [])):
                revision = harness.get("latestSuccessfulRevision", "")
                if re.fullmatch(r"[0-9a-f]{64}", revision):
                    return revision
        time.sleep(2)
    raise RuntimeError("new AgentTemplate did not become Ready; private profile was not changed")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("installation", type=Path)
    parser.add_argument("name", help="new attempt-exclusive AgentTemplate and Secret name")
    parser.add_argument("--apply", action="store_true", help="create local resources and select the new turn")
    args = parser.parse_args()
    workflow_path = args.installation / "workflow.json"
    overlay_path = args.installation / "kustomization.yaml"
    workflow = json.loads(workflow_path.read_text())
    overlay = overlay_path.read_text()
    current = workflow["conversation"]["engine"]["template"]
    if not args.apply:
        print("Current chat template:", current, "\nNext chat template:", args.name)
        return
    if args.name == current:
        raise ValueError("the current template cannot serve another turn")
    template = get("agenttemplate", current)
    model = get("modelconfig", template["spec"]["modelConfig"]["name"])
    for kind, name in (("secret", args.name), ("modelconfig", args.name + "-model"),
                       ("agenttemplate", args.name)):
        if kube("get", kind, name, "--ignore-not-found", "-o", "name").strip():
            raise ValueError("resource already exists: " + kind + "/" + name)
    resources = build_resources(template, model, args.name, secrets.token_urlsafe(32))
    for resource in resources:
        kube("create", "-f", "-", document=resource)
    revision = wait_revision(args.name)
    new_workflow, new_overlay = select_turn(workflow, overlay, args.name, revision)
    for path in (workflow_path, overlay_path):
        backup = path.with_name(path.name + ".before-" + args.name)
        backup.write_bytes(path.read_bytes())
        backup.chmod(0o600)
    workflow_path.write_text(json.dumps(new_workflow, indent=2) + "\n")
    overlay_path.write_text(new_overlay)
    subprocess.run(["python3", "scripts/check-installation.py", str(args.installation)],
                   capture_output=True, text=True, check=True)
    subprocess.run(["kubectl", "--context", "kind-marina-local", "apply", "-k", str(args.installation)],
                   capture_output=True, text=True, check=True)
    subprocess.run(["kubectl", "--context", "kind-marina-local", "-n", "marina-system",
                    "rollout", "restart", "deployment/marina"], capture_output=True, text=True, check=True)
    subprocess.run(["kubectl", "--context", "kind-marina-local", "-n", "marina-system",
                    "rollout", "status", "deployment/marina", "--timeout=60s"],
                   capture_output=True, text=True, check=True)
    print("Prepared", args.name, "at revision", revision, "and selected it in the private local installation")


if __name__ == "__main__":
    main()
