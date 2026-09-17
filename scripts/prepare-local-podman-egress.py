#!/usr/bin/env python3
"""Prepare a local controller egress IP snapshot; never apply it or enable intake."""
import argparse
from datetime import datetime, timezone
import ipaddress
import json
import os
from pathlib import Path
import subprocess

HOSTS = ("api.github.com", "github.com", "openrouter.ai")
KUBECTL = ["kubectl", "--context", "kind-marina-local", "--request-timeout=15s"]
PODMAN = ["podman", "--connection", "podman-machine-default-root"]


def build_policy(resolved, service_ip, endpoints):
    addresses = set()
    for host in HOSTS:
        if not resolved.get(host):
            raise ValueError(f"no addresses for {host}")
        for value in resolved[host]:
            address = ipaddress.ip_address(value)
            if address.version != 4 or not address.is_global:
                raise ValueError(f"non-public IPv4 address for {host}")
            addresses.add(str(address))
    if not endpoints:
        raise ValueError("no Kubernetes API endpoints")

    def rule(values, port):
        if not 1 <= port <= 65535:
            raise ValueError("invalid API port")
        return {"to": [{"ipBlock": {"cidr": str(ipaddress.ip_network(value))}}
                       for value in values], "ports": [{"protocol": "TCP", "port": port}]}

    rules = [rule(sorted(addresses), 443), rule([service_ip], 443)]
    rules.extend(rule([str(ipaddress.ip_address(address))], port)
                 for address, port in sorted(set(endpoints)))
    return {"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
            "metadata": {"name": "marina-acceptance-egress", "namespace": "marina-system"},
            "spec": {"podSelector": {"matchLabels": {"app.kubernetes.io/name": "marina"}},
                     "policyTypes": ["Egress"], "egress": rules}}


def build_deployment_patch(resolved):
    return {"apiVersion": "apps/v1", "kind": "Deployment",
            "metadata": {"name": "marina", "namespace": "marina-system"},
            "spec": {"template": {"spec": {"hostAliases": [
                {"ip": sorted(resolved[host])[0], "hostnames": [host]} for host in HOSTS]}}}}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("directory", type=Path, help="new output directory outside Git")
    args = parser.parse_args()
    output = args.directory.expanduser().resolve()
    root = Path(__file__).resolve().parents[1]
    if output == root or root in output.parents:
        parser.error("keep installation-specific address snapshots outside the repository")

    def run(command):
        return subprocess.run(command, check=True, text=True, capture_output=True, timeout=30).stdout

    resolved = {}
    for host in HOSTS:
        answer = run(PODMAN + ["exec", "marina-local-control-plane", "getent", "ahostsv4", host])
        resolved[host] = sorted({line.split()[0] for line in answer.splitlines() if line.strip()})
    service = json.loads(run(KUBECTL + ["-n", "default", "get", "service", "kubernetes", "-o", "json"]))
    slices = json.loads(run(KUBECTL + ["-n", "default", "get", "endpointslice", "-l",
                                     "kubernetes.io/service-name=kubernetes", "-o", "json"]))
    endpoints = [(address, port["port"]) for item in slices["items"]
                 for endpoint in item["endpoints"] if endpoint.get("conditions", {}).get("ready") is not False
                 for address in endpoint["addresses"] for port in item["ports"]
                 if port.get("protocol", "TCP") == "TCP"]
    policy = build_policy(resolved, service["spec"]["clusterIP"], endpoints)
    os.umask(0o077)
    output.mkdir(parents=True, exist_ok=False)
    (output / "policy.json").write_text(json.dumps(policy, indent=2) + "\n")
    (output / "marina-destinations.json").write_text(json.dumps(build_deployment_patch(resolved), indent=2) + "\n")
    evidence = {"observed_at": datetime.now(timezone.utc).isoformat(), "hosts": resolved,
                "api_service": service["spec"]["clusterIP"], "api_endpoints": endpoints,
                "resolver": "Kind node via Podman DNS; frozen in controller hostAliases",
                "context": "kind-marina-local"}
    (output / "destinations.json").write_text(json.dumps(evidence, indent=2) + "\n")
    print(f"Prepared policy.json, marina-destinations.json and destinations.json in {output}; no resources changed.")
    print("Keep existing Deck/DNS egress. Apply the policy and add the paired Deployment patch to the overlay.")
    print("This pins IPs, not provider ownership. Address retirement requires refreshing both files and rerunning network tests.")


if __name__ == "__main__":
    main()
