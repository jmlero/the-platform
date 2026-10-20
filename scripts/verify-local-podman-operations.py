#!/usr/bin/env python3
"""Rehearse renewed TLS and operator credential rotation locally.

Requires disabled intake and no active work. Only the dedicated local context is
used. Temporary credentials stay in memory and are revoked/restored in finally.
No Git/model request is made. Secret values and subprocess output are never logged.
"""
import argparse
import base64
import copy
import hashlib
import http.cookiejar
import json
from pathlib import Path
import secrets
import ssl
import subprocess
import time
import urllib.error
import urllib.request

KUBE = ["kubectl", "--context", "kind-marina-local", "-n", "marina-system"]
BASE = "https://localhost:8443"


def kube(*arguments, document=None):
    return subprocess.run(KUBE + list(arguments), input=None if document is None else json.dumps(document),
                          text=True, capture_output=True, check=True, timeout=60).stdout


def get(kind, name):
    return json.loads(kube("get", kind, name, "-o", "json"))


def replace_data(name, data):
    secret = get("secret", name)
    secret["data"] = data
    kube("replace", "-f", "-", document=secret)


def encoded(value):
    return base64.b64encode(value.encode()).decode()


def wait_for(check, description):
    deadline = time.monotonic() + 150
    while time.monotonic() < deadline:
        try:
            if check():
                return
        except (urllib.error.URLError, TimeoutError, subprocess.CalledProcessError):
            pass
        time.sleep(2)
    raise RuntimeError(description)


class Client:
    def __init__(self, installation):
        self.cookies = http.cookiejar.CookieJar()
        context = ssl.create_default_context(cafile=str(installation / "ca.crt"))
        self.opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context),
                                                 urllib.request.HTTPCookieProcessor(self.cookies))

    def request(self, path, payload=None, token=None, method=None):
        headers = {"Origin": BASE}
        if token:
            headers["Authorization"] = "Bearer " + token
        if payload is not None:
            headers["Content-Type"] = "application/json"
            for cookie in self.cookies:
                if cookie.name == "deck_operator_csrf":
                    headers["X-CSRF-Token"] = cookie.value
        request = urllib.request.Request(BASE + path, data=None if payload is None else json.dumps(payload).encode(),
                                         headers=headers, method=method)
        with self.opener.open(request, timeout=10) as response:
            return None if response.status == 204 else json.load(response)

    def login_status(self, token):
        try:
            self.request("/api/v1/operator/session", {"token": token})
            return 200
        except urllib.error.HTTPError as error:
            return error.code


def identities(bootstrap):
    return ({task["id"]: task["status"] for task in bootstrap["tasks"]},
            {agent["id"] for agent in bootstrap["agents"]})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("installation", type=Path)
    parser.add_argument("--install-renewed-certificates", action="store_true")
    args = parser.parse_args()
    installation = args.installation.expanduser().resolve()
    deployment = get("deployment", "marina")
    container = next(c for c in deployment["spec"]["template"]["spec"]["containers"] if c["name"] == "marina")
    if "--intake-enabled=false" not in container.get("args", []):
        raise RuntimeError("disable intake before this idle-installation rehearsal")
    client = Client(installation)
    operator = (installation / "operator-token").read_text().strip()
    if client.login_status(operator) != 200:
        raise RuntimeError("private installation credentials do not match running installation")
    before = client.request("/api/v1/bootstrap")
    if any(task["status"] in {"planning", "waiting_decision", "running", "verifying"} for task in before["tasks"]):
        raise RuntimeError("finish or cancel active work before this rehearsal")
    if args.install_renewed_certificates:
        originals = {name: get("secret", name)["data"] for name in ("deck-tls", "marina-worker-tls")}
        try:
            for service_name, secret_name in (("deck", "deck-tls"), ("marina", "marina-worker-tls")):
                data = copy.deepcopy(originals[secret_name])
                data.update({"tls.crt": encoded((installation / f"{service_name}.crt").read_text()),
                             "tls.key": encoded((installation / f"{service_name}.key").read_text())})
                replace_data(secret_name, data)
            for name in ("deck", "marina"):
                kube("rollout", "restart", "deployment/" + name)
                kube("rollout", "status", "deployment/" + name, "--timeout=55s")
            wait_for(lambda: client.request("/readyz")["status"] == "ready", "renewed TLS readiness")
            assert client.request("/api/v1/operator/session")["authenticated"]
            assert identities(client.request("/api/v1/bootstrap")) == identities(before)
            # Check the actual served leaf, not merely a still-valid old leaf.
            import socket
            with socket.create_connection(("localhost", 8443), timeout=10) as sock:
                with ssl.create_default_context(cafile=str(installation / "ca.crt")).wrap_socket(sock, server_hostname="localhost") as secure:
                    expected = ssl.PEM_cert_to_DER_cert((installation / "deck.crt").read_text())
                    assert secure.getpeercert(binary_form=True) == expected
            print("PASS renewed served Deck TLS, both service rollouts, session/task/agent preservation", flush=True)
        except Exception:
            for name, data in originals.items():
                replace_data(name, data)
            for name in ("deck", "marina"):
                kube("rollout", "restart", "deployment/" + name)
            raise

    # Deck reaches Marina with its projected Kubernetes token, which the
    # kubelet rotates; only the operator credential is rotated here.
    original_auth = get("secret", "deck-auth")["data"]
    original_pods = {p["metadata"]["uid"] for p in json.loads(kube("get", "pods", "-o", "json"))["items"]}
    temporary_operator = secrets.token_hex(32)
    auth = copy.deepcopy(original_auth)
    policy = json.loads(base64.b64decode(auth["operator-auth.json"]))
    old_hash = hashlib.sha256(operator.encode()).hexdigest()
    identity = next(i for i in policy["operators"] if old_hash in i["token_sha256"])
    identity["token_sha256"].append(hashlib.sha256(temporary_operator.encode()).hexdigest())
    auth["operator-auth.json"] = encoded(json.dumps(policy))
    temporary_client = Client(installation)

    def probe_login(token):
        probe = Client(installation)
        status = probe.login_status(token)
        if status == 200:
            probe.request("/api/v1/operator/session", {}, method="DELETE")
        return status

    try:
        replace_data("deck-auth", auth)
        wait_for(lambda: probe_login(temporary_operator) == 200, "overlapping credentials did not load")
        assert temporary_client.login_status(temporary_operator) == 200
        assert probe_login(operator) == 200
        print("PASS overlapping operator fingerprints reload without restart", flush=True)
        identity["token_sha256"].remove(old_hash)
        auth["operator-auth.json"] = encoded(json.dumps(policy))
        replace_data("deck-auth", auth)
        wait_for(lambda: probe_login(operator) == 401, "retired credential still accepted")
        assert client.request("/api/v1/operator/session")["authenticated"]
        assert temporary_client.request("/api/v1/operator/session")["authenticated"]
        assert identities(client.request("/api/v1/bootstrap")) == identities(before)
        assert {p["metadata"]["uid"] for p in json.loads(kube("get", "pods", "-o", "json"))["items"]} == original_pods
        print("PASS revoked old token, surviving sessions and stable identities", flush=True)
    finally:
        # Restore the original policy, then confirm the temporary token is revoked.
        replace_data("deck-auth", original_auth)
        wait_for(lambda: probe_login(temporary_operator) == 401, "temporary credential revocation")
        client.request("/api/v1/operator/session", {}, method="DELETE")
        temporary_client.request("/api/v1/operator/session", {}, method="DELETE")
        print("PASS original credential restored; temporary token and rehearsal sessions revoked", flush=True)

if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Never print HTTP bodies, Secret-bearing subprocess output or tokens.
        raise SystemExit(f"Operations rehearsal failed ({type(error).__name__}); inspect readiness and follow the recovery runbook.")
