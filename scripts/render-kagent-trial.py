#!/usr/bin/env python3
"""Render the locked KINT candidate; never install or alter a cluster.

Requires Helm and Ruby's standard YAML parser. Output is a JSON Kubernetes List.
The explicit --preflight option performs read-only local-cluster inspection.
"""
import argparse
import copy
import hashlib
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
PROFILE = ROOT / "deploy/kubernetes/kagent-trial"
NAMESPACES = ("ate-system", "podcertificate-controller-system", "kint-engine")
LABEL = {"the-platform.dev/trial": "kint-02"}


def run(args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs)


def image_key(ref):
    if "@" in ref:
        repo, digest = ref.split("@")
        return repo.rsplit(":", 1)[0] + "@" + digest
    return ref


def pin_images(value, images):
    if isinstance(value, dict):
        for key, item in value.items():
            if key in ("image", "workerImage", "pauseImage") and isinstance(item, str):
                if item not in images.values():
                    try:
                        value[key] = images[image_key(item)]
                    except KeyError as exc:
                        raise ValueError(f"unlocked image: {item}") from exc
            else:
                pin_images(item, images)
    elif isinstance(value, list):
        for item in value:
            pin_images(item, images)


def scope_secrets(role):
    """Keep required cluster watches, but never read other products' secrets."""
    scoped = []
    for rule in role.get("rules", []):
        if "secrets" not in rule.get("resources", []):
            continue
        secret_rule = copy.deepcopy(rule)
        secret_rule["resources"] = ["secrets"]
        rule["resources"].remove("secrets")
        namespaces = ["kint-engine"] if "credential-provider" in role["metadata"]["name"] else ["ate-system"]
        for namespace in namespaces:
            scoped.append({"apiVersion": role["apiVersion"], "kind": "Role",
                           "metadata": {"name": role["metadata"]["name"] + "-secrets",
                                        "namespace": namespace}, "rules": [secret_rule]})
    role["rules"] = [rule for rule in role.get("rules", []) if rule.get("resources")]
    return scoped


def certificate_api_ready(group):
    return any(v["groupVersion"] == "certificates.k8s.io/v1beta1"
               for v in group.get("versions", []))


def scope_mutations(role):
    scoped = []
    if role["metadata"]["name"] != "ate-controller":
        return scoped
    rules = []
    for rule in role.get("rules", []):
        if not set(rule.get("resources", [])).intersection(
                {"deployments", "workerpools", "workerpools/finalizers",
                 "workerpools/status", "networkpolicies"}):
            continue
        local = copy.deepcopy(rule)
        local["verbs"] = [v for v in rule["verbs"] if v not in ("get", "list", "watch")]
        rule["verbs"] = [v for v in rule["verbs"] if v in ("get", "list", "watch")]
        if local["verbs"]:
            rules.append(local)
    role["rules"] = [r for r in role["rules"] if r["verbs"]]
    if rules:
        scoped.append({"apiVersion": role["apiVersion"], "kind": "Role",
                       "metadata": {"name": "ate-controller-mutations", "namespace": "kint-engine"},
                       "rules": rules})
    return scoped


def render(helm, directory, lock):
    objects = []
    images = {image_key(x["upstream"]): x["pinned"] for x in lock["images"]}
    for chart in lock["charts"]:
        path = directory / chart["archive"]
        if hashlib.sha256(path.read_bytes()).hexdigest() != chart["sha256"]:
            raise ValueError(f"chart archive checksum mismatch: {path.name}")
        substrate = chart["name"].startswith("substrate")
        command = [helm, "template", "substrate" if substrate else "kint-engine",
                   str(path), "--namespace", "ate-system" if substrate else "kint-engine"]
        if not chart["name"].endswith("-crds"):
            command += ["-f", str(PROFILE / ("substrate-values.yaml" if substrate else "kagent-values.yaml"))]
        raw = run(command)
        documents = json.loads(run(["ruby", "-ryaml", "-rjson", "-e",
                                   "puts JSON.generate(YAML.load_stream(STDIN.read))"], input=raw))
        for obj in documents:
            if not obj:
                continue
            # The chart's default ModelConfig has no governed credential.
            if obj["kind"] == "ModelConfig":
                continue
            pin_images(obj, images)
            objects.append(obj)
    scoped = []
    for obj in objects:
        if obj["kind"] == "ClusterRole":
            for role in scope_secrets(obj) + scope_mutations(obj):
                scoped.append(role)
                bindings = [x for x in objects if x["kind"] == "ClusterRoleBinding"
                            and x["roleRef"]["name"] == obj["metadata"]["name"]]
                for binding in bindings:
                    scoped.append({"apiVersion": role["apiVersion"], "kind": "RoleBinding",
                                   "metadata": copy.deepcopy(role["metadata"]),
                                   "roleRef": {"apiGroup": "rbac.authorization.k8s.io",
                                               "kind": "Role", "name": role["metadata"]["name"]},
                                   "subjects": copy.deepcopy(binding["subjects"])})
    objects.extend(scoped)
    for namespace in NAMESPACES:
        if not any(x["kind"] == "Namespace" and x["metadata"]["name"] == namespace for x in objects):
            objects.insert(0, {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": namespace}})
    for obj in objects:
        namespace = obj["metadata"].get("namespace")
        if namespace and namespace not in NAMESPACES:
            raise ValueError(f"unexpected namespace: {namespace}")
        obj["metadata"].setdefault("labels", {}).update(LABEL)
        if obj["kind"] in ("Deployment", "DaemonSet", "StatefulSet", "Job"):
            if obj["kind"] == "Deployment":
                obj["spec"]["replicas"] = 1
            template = obj["spec"]["template"]
            template.setdefault("metadata", {}).setdefault("labels", {}).update(LABEL)
            for container in template["spec"].get("containers", []):
                resources = container.setdefault("resources", {})
                resources.setdefault("requests", {"cpu": "50m", "memory": "64Mi"})
                resources.setdefault("limits", {"cpu": "1", "memory": "1Gi" if obj["metadata"]["name"] == "rustfs" else "512Mi"})
    return {"apiVersion": "v1", "kind": "List", "items": objects}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("charts", type=Path)
    parser.add_argument("--helm", default="helm")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--preflight", action="store_true")
    args = parser.parse_args()
    lock = json.loads((PROFILE / "lock.json").read_text())
    result = render(args.helm, args.charts, lock)
    if args.preflight:
        group = json.loads(run(["kubectl", "--context", "kind-marina-local", "get", "--raw",
                               "/apis/certificates.k8s.io"]))
        if not certificate_api_ready(group):
            raise SystemExit("BLOCKED: Substrate beta4 requires certificates.k8s.io/v1beta1; "
                             "this cluster does not serve it. No cluster changes made.")
    args.output.write_text(json.dumps(result, indent=2) + "\n")
    print(f"Rendered {len(result['items'])} candidate resources to {args.output}; not installed. "
          "Network, credentials, collision checks and live acceptance remain required.")


if __name__ == "__main__":
    main()
