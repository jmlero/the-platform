#!/usr/bin/env python3
"""Install the locked KINT trial only into unused local Kind resources.

Preparation egress stays open for atelet until the explicit runtime gate closes
it. This helper never changes the host, task intake, or the existing platform.
"""
import argparse
import base64
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
PROFILE = ROOT / "deploy/kubernetes/kagent-trial"
KUBE = ["kubectl", "--context", "kind-marina-local"]


def run(args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs)


def apply(items):
    result = run(KUBE + ["apply", "--server-side", "--field-manager=kint-02", "-f", "-"],
                 input=json.dumps({"apiVersion": "v1", "kind": "List", "items": items}))
    print(result, end="")


def check_collisions(desired, existing):
    identities = {(x["kind"], x["metadata"].get("namespace"), x["metadata"]["name"])
                  for x in existing}
    for obj in desired:
        identity = (obj["kind"], obj["metadata"].get("namespace"), obj["metadata"]["name"])
        if identity in identities:
            raise ValueError(f"resource collision: {identity}; refusing to adopt existing resources")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("assets", type=Path)
    parser.add_argument("--helm", required=True)
    args = parser.parse_args()
    assets = args.assets.resolve()
    spec = importlib.util.spec_from_file_location("trial", ROOT / "scripts/render-kagent-trial.py")
    trial = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(trial)
    lock = json.loads((PROFILE / "lock.json").read_text())
    for asset in lock["cli_assets"]:
        if hashlib.sha256((assets / asset["name"]).read_bytes()).hexdigest() != asset["sha256"]:
            raise ValueError("CLI checksum mismatch: " + asset["name"])
    objects = trial.render(args.helm, assets, lock)["items"]
    if not trial.certificate_api_ready(json.loads(run(KUBE + ["get", "--raw", "/apis/certificates.k8s.io"]))):
        raise ValueError("required beta certificate API is unavailable")
    # The profile's NetworkPolicy deliberately pins this existing local node.
    node = json.loads(run(KUBE + ["get", "node", "marina-local-control-plane", "-o", "json"]))
    addresses = {x["address"] for x in node["status"]["addresses"] if x["type"] == "InternalIP"}
    if addresses != {"10.89.0.2"} or node["status"]["nodeInfo"]["architecture"] != "arm64":
        raise ValueError("node address or architecture differs from the locked local profile")
    run(["podman", "--connection", "podman-machine-default-root", "inspect", "marina-local-control-plane"])
    existing = []
    for resource in ("namespaces", "crd", "clusterrole", "clusterrolebinding", "priorityclass",
                     "validatingadmissionpolicy", "validatingadmissionpolicybinding"):
        existing.extend(json.loads(run(KUBE + ["get", resource, "-o", "json"]))["items"])
    check_collisions(objects, existing)
    namespaces = [o for o in objects if o["kind"] == "Namespace"]
    apply(namespaces)
    print(run(KUBE + ["apply", "-f", str(PROFILE / "network-policy.json")]), end="")
    apply([{"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
            "metadata": {"name": "kint-preparation-downloads", "namespace": "ate-system",
                         "labels": trial.LABEL},
            "spec": {"podSelector": {"matchLabels": {"app": "atelet"}},
                     "policyTypes": ["Egress"], "egress": [{}]}}])
    crds = [o for o in objects if o["kind"] == "CustomResourceDefinition"]
    apply(crds)
    for obj in crds:
        run(KUBE + ["wait", "--for=condition=Established", "crd/" + obj["metadata"]["name"], "--timeout=30s"])
    pools = [("service-dns-ca-pool", "podcertificate-controller-system", "ca"),
             ("pod-identity-ca-pool", "podcertificate-controller-system", "ca"),
             ("actor-id-jwt-pool", "ate-system", "jwt"),
             ("actor-id-ca-pool", "ate-system", "ca"),
             ("egress-mitm-ca-pool", "ate-system", "ca")]
    for name, namespace, kind in pools:
        command = [str(assets / "kubectl-ate"), "--context", "kind-marina-local", "admin",
                   "make-" + kind + "-pool", "--" + ("ca-id" if kind == "ca" else "key-id") + "=1",
                   "--name=" + name, "--secret-namespace=" + namespace]
        if name == "egress-mitm-ca-pool":
            command.append("--key-type=ECDSAP256")
        subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    # Only the public certificate is copied into the derived Secret. The pool is never printed or saved.
    secret = json.loads(run(KUBE + ["-n", "ate-system", "get", "secret", "actor-id-ca-pool", "-o", "json"]))
    pool = json.loads(base64.b64decode(secret["data"]["pool"]))
    cert = base64.b64decode(pool["CAs"][0]["RootCertificateDER"])
    pem = subprocess.check_output(["openssl", "x509", "-inform", "der", "-outform", "pem"], input=cert).decode()
    apply([{"apiVersion": "v1", "kind": "Secret",
            "metadata": {"name": "actor-id-ca-certs", "namespace": "ate-system", "labels": trial.LABEL},
            "stringData": {"ca.crt": pem}}])
    issuer = json.loads(run(KUBE + ["get", "--raw", "/.well-known/openid-configuration"]))["issuer"]
    auth = {"actorIdentityJWTProvider": "kubernetes", "jwtProviders": [{
        "name": "kubernetes", "issuer": issuer, "audiences": ["api.ate-system.svc"],
        "certificateAuthorityFile": "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
        "discoveryTokenFile": "/var/run/secrets/kubernetes.io/serviceaccount/token"}]}
    apply([{"apiVersion": "v1", "kind": "ConfigMap",
            "metadata": {"name": "ate-api-authentication", "namespace": "ate-system", "labels": trial.LABEL},
            "data": {"authentication.yaml": json.dumps(auth)}}])
    apply([o for o in objects if o["kind"] not in ("Namespace", "CustomResourceDefinition")])
    print("Trial installed. Finish preparation, close kint-preparation-downloads, then run live acceptance.")


if __name__ == "__main__":
    main()
