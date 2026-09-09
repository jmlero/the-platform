#!/usr/bin/env python3
"""Prepare local installation files. Does not contact a cluster or a provider."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="new private directory outside the repository")
    parser.add_argument("--local-podman", action="store_true", help="local images and TLS services with new work intake disabled")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    output = args.directory.expanduser().resolve()
    if output == root or root in output.parents:
        parser.error("write credentials outside the repository")
    os.umask(0o077)
    output.mkdir(parents=True, exist_ok=False)

    def write(name, value):
        (output / name).write_text(value)

    def token(name):
        value = secrets.token_hex(32)
        write(name, value + "\n")
        return hashlib.sha256(value.encode()).hexdigest()

    service_hash = token("appdeck-token")
    operator_hash = token("operator-token")
    token("artifact-token")
    token("worker-signing-key")
    write("git-token", "")
    write("model-token", "")
    capabilities = ["provider.info", "agent.register", "work.ready.list", "work.inspect",
                    "work.claim.atomic", "work.claim.lookup", "run.assigned.list", "run.lease.renew",
                    "run.plan.submit", "run.start", "run.verify", "run.complete", "run.block",
                    "run.progress.append", "command.list", "command.ack", "event.append",
                    "decision.inspect", "evidence.record"]
    write("service-auth.json", json.dumps({"version": 1, "identities": [{"id": "marina",
          "token_sha256": [service_hash], "projects": ["prj_default"], "capabilities": capabilities}]}, indent=2) + "\n")
    write("operator-auth.json", json.dumps({"version": 1, "operators": [{"id": "owner", "token_sha256": [operator_hash]}]}, indent=2) + "\n")
    write("workflow.json", (root / "deploy/kubernetes/first-release/workflow.example.json").read_text())

    def openssl(*arguments):
        subprocess.run(["openssl", *arguments], cwd=output, check=True, capture_output=True)

    openssl("req", "-x509", "-newkey", "rsa:3072", "-nodes", "-days", "365", "-keyout", "ca.key",
            "-out", "ca.crt", "-subj", "/CN=Marina installation CA", "-addext", "basicConstraints=critical,CA:TRUE",
            "-addext", "keyUsage=critical,keyCertSign,cRLSign")
    for service, port in (("marina", 8090), ("appdeck", 8080)):
        name = f"{service}.marina-system.svc"
        openssl("req", "-newkey", "rsa:3072", "-nodes", "-keyout", f"{service}.key", "-out", f"{service}.csr", "-subj", f"/CN={name}")
        write(f"{service}.ext", f"subjectAltName=DNS:{name},DNS:{name}.cluster.local,DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=serverAuth\n")
        openssl("x509", "-req", "-in", f"{service}.csr", "-CA", "ca.crt", "-CAkey", "ca.key", "-CAcreateserial",
                "-out", f"{service}.crt", "-days", "90", "-sha256", "-extfile", f"{service}.ext")
    profile = "local-podman" if args.local_podman else "first-release"
    base = json.dumps(os.path.relpath(root / "deploy/kubernetes" / profile, output))
    registry = "localhost/the-platform" if args.local_podman else "YOUR_REGISTRY"
    tag = "local" if args.local_podman else "YOUR_RELEASE"
    write("kustomization.yaml", f"""apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: [{base}]
generatorOptions:
  disableNameSuffixHash: true
configMapGenerator:
  - name: marina-workflow
    namespace: marina-system
    files: [workflow.json]
  - name: marina-worker-ca
    namespace: marina-system
    files: [ca.crt]
  - name: marina-worker-ca
    namespace: marina-workers
    files: [ca.crt]
secretGenerator:
  - name: appdeck-auth
    namespace: marina-system
    files: [service-auth.json, operator-auth.json]
  - name: marina-credentials
    namespace: marina-system
    files: [appdeck-token, git-token, model-token, worker-signing-key]
  - name: marina-artifact-auth
    namespace: marina-system
    files: [token=artifact-token]
  - name: marina-worker-tls
    namespace: marina-system
    type: kubernetes.io/tls
    files: [tls.crt=marina.crt, tls.key=marina.key]
  - name: appdeck-tls
    namespace: marina-system
    type: kubernetes.io/tls
    files: [tls.crt=appdeck.crt, tls.key=appdeck.key]
# Set the AppDeck and Marina images you built. Use immutable digests for releases.
images:
  - name: ghcr.io/jmlero/the-platform-appdeck
    newName: {registry}/appdeck
    newTag: {tag}
  - name: ghcr.io/jmlero/the-platform-marina
    newName: {registry}/marina
    newTag: {tag}
""")
    print(f"Prepared private installation files in {output}.")
    if args.local_podman:
        print("Local services use TLS with new work intake disabled. workflow.json is a pending template until repository/model setup is complete.")
    else:
        print("Edit workflow.json, image references and provider token files before installation.")
    print("Operator login credential: operator-token. No cluster or provider was contacted.")


if __name__ == "__main__":
    main()
