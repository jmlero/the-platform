#!/usr/bin/env python3
"""Verify the local TLS/login boundary and both stores across Deployment restarts.

Creates one unauthorized verification task, confirms disabled intake leaves it
ready, then cancels it. Does not contact a source or model provider.
"""
import argparse
import http.cookiejar
import json
from pathlib import Path
import ssl
import subprocess
import urllib.error
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("installation", type=Path)
args = parser.parse_args()
base = "https://localhost:8443"
context = ssl.create_default_context(cafile=str(args.installation / "ca.crt"))
cookies = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=context),
                                   urllib.request.HTTPCookieProcessor(cookies))


def request(path, payload=None, method=None):
    headers = {"Origin": base}
    if payload is not None:
        headers["Content-Type"] = "application/json"
        for cookie in cookies:
            if cookie.name == "appdeck_operator_csrf":
                headers["X-CSRF-Token"] = cookie.value
    body = None if payload is None else json.dumps(payload).encode()
    with opener.open(urllib.request.Request(base + path, data=body, headers=headers, method=method), timeout=10) as response:
        return None if response.status == 204 else json.load(response)


assert request("/readyz")["status"] == "ready"
try:
    request("/api/v1/bootstrap")
    raise AssertionError("operator API allowed unauthenticated access")
except urllib.error.HTTPError as error:
    assert error.code == 401, error.code
session = request("/api/v1/operator/session", {"token": (args.installation / "operator-token").read_text().strip()})
assert session["authenticated"]
before = request("/api/v1/bootstrap")
agent_ids = {agent["id"] for agent in before["agents"]}
assert agent_ids, "Marina has not registered"
task_input = {
    "project_id": "prj_default", "title": "Local installation verification",
    "description": "Infrastructure check only. No repository or model authority. Cancelled after verification.",
    "acceptance_criteria": "Task and Marina identity survive both service restarts; disabled intake creates no run.",
    "status": "ready", "priority": "low", "budget_minutes": 30,
}
task = next((item for item in before["tasks"] if item["title"] == task_input["title"]
             and item["description"] == task_input["description"] and item["status"] == "cancelled"), None)
if task is None:
    task = request("/api/v1/tasks", task_input)
else:
    task = request(f"/api/v1/tasks/{task['id']}/move", {"status": "ready"})
kubectl = ["kubectl", "--context", "kind-marina-local", "-n", "marina-system"]
for deployment in ("appdeck", "marina"):
    subprocess.run(kubectl + ["rollout", "restart", f"deployment/{deployment}"], check=True)
    subprocess.run(kubectl + ["rollout", "status", f"deployment/{deployment}", "--timeout=55s"], check=True)
assert request("/api/v1/operator/session")["authenticated"], "session did not survive Deck restart"
after = request("/api/v1/bootstrap")
assert {agent["id"] for agent in after["agents"]} == agent_ids, "Marina identity changed on restart"
detail = request(f"/api/v1/operator/tasks/{task['id']}")
assert detail["task"]["status"] == "ready", "disabled intake claimed the task"
assert not detail["runs"], "disabled intake created a run"
request(f"/api/v1/tasks/{task['id']}/move", {"status": "cancelled"})
request("/api/v1/operator/session", {}, method="DELETE")
print("PASS trusted TLS, operator authentication, persistent session/task/Marina identity, and disabled intake.")
print(f"Verification task {task['id']} is cancelled; no repository or model was contacted.")
