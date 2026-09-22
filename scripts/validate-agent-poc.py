#!/usr/bin/env python3
"""Bounded local AGENT-06 / AGENT-13 proof (--mvp selects the latter). Explicit actions; no credentials in evidence.

Only the existing kind-marina-local / podman-machine-default-root installation
and jmlero/marina-acceptance repository are used. Intake is disabled again as soon
as the selected task is claimed. No merge, base-branch write or deployment outside
this development profile is performed.
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import ssl
import subprocess
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
INSTALL = Path.home() / ".local/share/the-platform/marina-local/installation"
OUT = Path("/private/tmp/platform-agent-poc-20260921")
BASE = "https://localhost:8443"
KUBE = ["kubectl", "--context", "kind-marina-local", "--request-timeout=20s"]
PODMAN = ["podman", "--connection", "podman-machine-default-root"]
PREFIX = "Agent PoC 2026-09-21 "
ACTIVE = {"planning", "waiting_decision", "running", "verifying"}
TERMINAL = {"done", "blocked", "failed", "cancelled"}


def main():
    global OUT, PREFIX
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=["inspect", "load-images", "install", "associate", "start", "watch", "result", "network", "disable-intake", "source", "cancel", "restart"])
    parser.add_argument("value", nargs="?")
    parser.add_argument("--mvp", action="store_true")
    parser.add_argument("--exercise", choices=["normal", "restart-deck", "restart-marina", "cancel", "token-budget", "restart-marina-planning", "restart-deck-planning"], default="normal")
    args = parser.parse_args()
    if args.mvp:
        OUT = Path("/private/tmp/platform-agent-mvp-20260922")
        PREFIX = "Agent MVP 2026-09-22 "
    release = "agent-mvp-20260922" if args.mvp else "agent-poc-20260921"
    binary = "/private/tmp/platform-agent-mvp-marina" if args.mvp else "/private/tmp/platform-agent-marina"
    OUT.mkdir(mode=0o700, exist_ok=True)
    os.umask(0o077)
    secrets = [(INSTALL / name).read_text().strip() for name in ("operator-token", "git-token", "model-token", "deck-token", "worker-signing-key", "artifact-token")]

    def safe(value):
        text = value if isinstance(value, str) else json.dumps(value, indent=2)
        for secret in secrets:
            if secret:
                text = text.replace(secret, "[REDACTED]")
        return text

    def emit(value):
        print(safe(value), flush=True)

    def save(name, value):
        (OUT / name).write_text(safe(value) + "\n")

    def cmd(command, data=None, timeout=55):
        result = subprocess.run(command, input=data, text=True, capture_output=True, timeout=timeout)
        if result.returncode:
            raise RuntimeError(safe(result.stderr[-2000:]))
        return result.stdout

    def kube(*arguments):
        return cmd(KUBE + list(arguments))

    def resource(namespace, kind):
        return json.loads(kube("-n", namespace, "get", kind, "-o", "json"))

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *_, **__):
            return None

    cookies = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(INSTALL / "ca.crt"))), urllib.request.HTTPCookieProcessor(cookies), NoRedirect())

    def api(path, body=None, method=None, raw=False, key=None):
        headers = {"Origin": BASE}
        if key:
            headers["Idempotency-Key"] = key
        if body is not None:
            headers["Content-Type"] = "application/json"
            for cookie in cookies:
                if cookie.name == "deck_operator_csrf":
                    headers["X-CSRF-Token"] = cookie.value
        request = urllib.request.Request(BASE + path, data=None if body is None else json.dumps(body).encode(), method=method, headers=headers)
        try:
            with opener.open(request, timeout=20) as response:
                body = response.read()
                return body if raw else (json.loads(body) if body else None)
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"Deck HTTP {error.code}: " + safe(error.read().decode()[:1000])) from None

    def login():
        api("/api/v1/operator/session", {"token": secrets[0]})

    def detail(task_id):
        value = api("/api/v1/operator/tasks/" + task_id)
        save(task_id + "-detail.json", value)
        return value

    def set_intake(enabled, task_id=None):
        assert not enabled or (task_id and task_id.startswith("tsk_"))
        arguments = ["serve", "--intake-task-id", task_id] if enabled else ["serve", "--intake-enabled=false"]
        patch = [{"op": "replace", "path": "/spec/template/spec/containers/0/args", "value": arguments}]
        kube("-n", "marina-system", "patch", "deployment", "marina", "--type=json", "-p", json.dumps(patch))
        kube("-n", "marina-system", "rollout", "status", "deployment/marina", "--timeout=50s")

    def assert_idle():
        bootstrap = api("/api/v1/bootstrap")
        busy = [t["id"] for t in bootstrap["tasks"] if t["status"] in ACTIVE]
        assert not busy, "Existing work must settle first: " + str(busy)
        return bootstrap

    try:
        if args.action == "load-images":
            refs = {}
            for name in (("deck", "marina", "marina-analysis", "marina-worker") if args.mvp else ("deck", "marina", "marina-analysis")):
                tag = f"localhost/the-platform/{name}:{release}"
                archive = OUT / (name + ".tar")
                cmd(PODMAN + ["save", "--format", "oci-archive", "-o", str(archive), tag], timeout=180)
                cmd(PODMAN + ["cp", str(archive), "marina-local-control-plane:/tmp/" + archive.name], timeout=180)
                cmd(PODMAN + ["exec", "marina-local-control-plane", "ctr", "-n", "k8s.io", "images", "import", "/tmp/" + archive.name], timeout=180)
                lines = cmd(PODMAN + ["exec", "marina-local-control-plane", "ctr", "-n", "k8s.io", "images", "ls"]).splitlines()
                digest = next(line.split()[2] for line in lines if line.split()[0] == tag)
                assert digest.startswith("sha256:") and len(digest) == 71
                ref = f"localhost/the-platform/{name}@{digest}"
                if not any(line.split()[0] == ref for line in lines):
                    cmd(PODMAN + ["exec", "marina-local-control-plane", "ctr", "-n", "k8s.io", "images", "tag", tag, ref])
                refs[name] = ref
                emit({"loaded": name, "image": ref})
            save("images.json", refs)
            return
        login()
        if args.action == "inspect":
            bootstrap = api("/api/v1/bootstrap")
            deployments = resource("marina-system", "deployments")
            summary = {"tasks": [{k: t.get(k) for k in ("id", "title", "status")} for t in bootstrap["tasks"] if t["status"] in ACTIVE | {"ready"}], "deployments": [{"name": d["metadata"]["name"], "image": d["spec"]["template"]["spec"]["containers"][0]["image"], "args": d["spec"]["template"]["spec"]["containers"][0].get("args", [])} for d in deployments["items"]]}
            save("installation-observed.json" if (OUT / "installation-before.json").exists() else "installation-before.json", summary)
            if args.mvp:
                summary["pvcs"] = [{"name": p["metadata"]["name"], "uid": p["metadata"]["uid"], "volume": p["spec"]["volumeName"]} for p in resource("marina-system", "pvc")["items"]]
                summary["tasks_preserved"] = [{k: t.get(k) for k in ("id", "title", "status")} for t in bootstrap["tasks"]]
                name = "preservation-after.json" if (OUT / "preservation-before.json").exists() else "preservation-before.json"
                save(name, summary)
            emit(summary)
        elif args.action == "install":
            assert_idle()
            refs = json.loads((OUT / "images.json").read_text())
            config_path, overlay_path = INSTALL / "workflow.json", INSTALL / "kustomization.yaml"
            auth_path = INSTALL / "service-auth.json"
            for path in (config_path, overlay_path, auth_path):
                backup = OUT / ("before-" + path.name)
                if not backup.exists():
                    backup.write_bytes(path.read_bytes())
            config = json.loads(config_path.read_text())
            assert config["git"]["repository"] == "jmlero/marina-acceptance"
            assert config["git"]["author_email"] == "jmlero@hotmail.com"
            old_analysis = config.get("analysis")
            config["version"] = 1
            config["analysis"] = {"version": 1, "revision": 1, "image": refs["marina-analysis"], "harness_version": "langchain-create-agent/v1", "max_tokens": 20000, "budget_minutes": 5, "fixture": json.loads((ROOT / "marina/internal/analysis/fixtures/blocked-runs-v1.json").read_text())}
            if args.mvp:
                if old_analysis:
                    config["analysis"] = old_analysis
                config["kubernetes"]["image"] = refs["marina-worker"]
            config_path.write_text(json.dumps(config, indent=2) + "\n")
            auth = json.loads(auth_path.read_text())
            identity = next(i for i in auth["identities"] if i["id"] == "marina")
            assert identity["projects"] == ["prj_default"]
            for capability in ("context.read", "repository.read", "repository.write", "workload.publish"):
                if capability not in identity["capabilities"]:
                    identity["capabilities"].append(capability)
            auth_path.write_text(json.dumps(auth, indent=2) + "\n")
            if args.mvp:
                emit(cmd(["python3", str(ROOT / "scripts/prepare-agent-installation.py"), str(INSTALL), "--analysis-image", refs["marina-analysis"], "--marina-binary", binary, "--apply"]))
                config = json.loads(config_path.read_text())
            overlay = overlay_path.read_text()
            for name in ("deck", "marina"):
                current = resource("marina-system", "deployment/" + name)["spec"]["template"]["spec"]["containers"][0]["image"]
                old_digest = current.split("@")[1]
                new_digest = refs[name].split("@")[1]
                assert old_digest in overlay, "Current image differs from private installation overlay"
                overlay = overlay.replace(old_digest, new_digest)
            overlay_path.write_text(overlay)
            # The persisted installation overlay must continue to disable intake.
            rendered = cmd(["kubectl", "kustomize", str(INSTALL)])
            assert "--intake-enabled=false" in rendered
            cmd(["python3", str(ROOT / "scripts/check-installation.py"), str(INSTALL)])
            emit(kube("apply", "-k", str(INSTALL)))
            # Workflow ConfigMaps have stable names; changing only a worker pin
            # does not change the Deployment template or reload process config.
            if args.mvp:
                kube("-n", "marina-system", "rollout", "restart", "deployment/marina")
            for name in ("deck", "marina"):
                emit(kube("-n", "marina-system", "rollout", "status", "deployment/" + name, "--timeout=50s"))
            summary = json.loads(cmd([binary, "workload-profile", "--config", str(config_path)]))
            save("profile.json", summary)
            save("configuration.json", {"sha256": hashlib.sha256(config_path.read_bytes()).hexdigest(), "coding_image": config["kubernetes"]["image"], "model": {k: v for k, v in config["model"].items() if k != "token_file"}, "analysis": summary, "intake_enabled": False})
            emit(summary)
        elif args.action == "source":
            assert args.mvp
            source = {"id": "agent-mvp-guidance", "kind": "instruction", "title": "Bounded acceptance guidance", "content": "Keep the existing public API stable. For diagnostic analysis, call read_context_source for this instruction URI before producing DiagnosticReport; report only the synthetic fixture findings. No source or infrastructure mutation is authorized for analysis.", "trust": "guidance", "state": "active"}
            if args.value == "inactive":
                source.update(expected_revision=1, state="inactive")
            emit(api("/api/v1/operator/projects/prj_default/context/sources", source, "PUT"))
        elif args.action == "restart":
            assert args.value in ("deck", "marina")
            kube("-n", "marina-system", "rollout", "restart", "deployment/" + args.value)
            emit(kube("-n", "marina-system", "rollout", "status", "deployment/" + args.value, "--timeout=50s"))
        elif args.action == "cancel":
            d = detail(args.value)
            assert d["task"]["title"].startswith(PREFIX)
            emit(api("/api/v1/operator/runs/" + d["runs"][0]["id"] + "/commands", {"type": "cancel", "reason": "MVP acceptance cancellation"}))
        elif args.action == "associate":
            assert_idle()
            path = "/api/v1/operator/projects/prj_default/repository"
            current = api(path)
            deadline = time.monotonic() + 45
            while current.get("status") == "pending":
                assert time.monotonic() < deadline, "Existing association is still pending"
                time.sleep(1)
                current = api(path)
            request = {"expected_revision": current.get("revision", 0), "expected_binding_version": (current.get("binding") or {}).get("version", 0), "repository": "jmlero/marina-acceptance", "base_branch": "main"}
            response = api(path, request, "PUT")
            for _ in range(2):
                deadline = time.monotonic() + 45
                while response.get("status") == "pending":
                    assert time.monotonic() < deadline, "Repository association did not settle"
                    time.sleep(1)
                    response = api(path)
                if response.get("status") == "connected":
                    break
                binding = response.get("binding") or {}
                assert binding.get("repository") == "jmlero/marina-acceptance", "Unexpected repository in refreshed binding"
                request.update(expected_revision=response["revision"], expected_binding_version=binding["version"])
                response = api(path, request, "PUT")
            assert response.get("status") == "connected", "Association requires inspection"
            save("repository-association.json", response)
            emit(response)
        elif args.action == "disable-intake":
            set_intake(False)
            emit({"intake_enabled": False})
        elif args.action == "network":
            image = json.loads((OUT / "images.json").read_text())["marina-analysis"]
            probe = """import os, socket, ssl, urllib.request
assert not os.path.exists('/var/run/secrets/kubernetes.io/serviceaccount/token')
isolated = os.environ['ISOLATED'] == 'true'
if isolated:
    context = ssl.create_default_context(cafile='/run/ca/ca.crt')
    with urllib.request.urlopen('https://marina.marina-system.svc:8090/healthz', context=context, timeout=5) as r: assert r.status == 200
    print('PASS trusted TLS connection to Marina')
for host in ('kubernetes.default.svc', '1.1.1.1'):
    try:
        with socket.create_connection((host, 443), timeout=3): reachable = True
    except OSError: reachable = False
    assert reachable != isolated, 'unexpected reachability: ' + host
    print('PASS', host, 'blocked' if isolated else 'baseline reachable')
print('PASS no Kubernetes credential')
"""
            for namespace, isolated in (("marina-system", False), ("marina-workers", True)):
                name = "agent-network-" + str(time.time_ns())
                pod = {"restartPolicy": "Never", "automountServiceAccountToken": False, "enableServiceLinks": False,
                       "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532, "seccompProfile": {"type": "RuntimeDefault"}},
                       "containers": [{"name": "probe", "image": image, "imagePullPolicy": "Never", "command": ["python3", "-c", probe], "env": [{"name": "ISOLATED", "value": str(isolated).lower()}],
                                       "resources": {"requests": {"cpu": "100m", "memory": "128Mi"}, "limits": {"cpu": "1", "memory": "512Mi"}},
                                       "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
                                       "volumeMounts": [{"name": "ca", "mountPath": "/run/ca", "readOnly": True}]}],
                       "volumes": [{"name": "ca", "configMap": {"name": "marina-worker-ca"}}]}
                job = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": name, "namespace": namespace},
                       "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 40, "ttlSecondsAfterFinished": 120,
                                "template": {"metadata": {"labels": {"app.kubernetes.io/name": "marina-worker"}}, "spec": pod}}}
                try:
                    cmd(KUBE + ["create", "-f", "-"], json.dumps(job))
                    kube("-n", namespace, "wait", "--for=condition=complete", "job/" + name, "--timeout=50s")
                    logs = kube("-n", namespace, "logs", "job/" + name)
                    save("network-" + namespace + ".json", {"job": job, "observed": resource(namespace, "job/" + name), "logs": logs})
                    emit({"namespace": namespace, "logs": logs})
                finally:
                    kube("-n", namespace, "delete", "job", name, "--ignore-not-found=true", "--wait=false")
        elif args.action == "start":
            assert args.value in ("analysis", "coding")
            bootstrap = assert_idle()
            save(args.value + "-unrelated-ready.json", [t for t in bootstrap["tasks"] if t["status"] == "ready"])
            profile = json.loads((OUT / "profile.json").read_text())
            analysis = args.value == "analysis"
            cost = (10 if analysis else 20) if args.mvp else (5 if analysis else 20)
            previous = [json.loads(p.read_text()) for p in OUT.glob("*-task.json")]
            assert sum(t["budget"]["max_cost_minor"] for t in previous) + cost <= (180 if args.mvp else 28), "PoC allocation exceeds the remaining conservative US$3 envelope"
            config = json.loads((INSTALL / "workflow.json").read_text())
            assert config["require_approval"]
            body = {"project_id": "prj_default", "title": PREFIX + args.value + " " + args.exercise, "description": "Analyze only the installed synthetic blocked-runs diagnostic fixture. Do not mutate source or infrastructure." if analysis else "In src/clamp.cjs, clamp(value, minimum, maximum) wrongly returns maximum - 1 when value >= maximum. Change that branch to return maximum. Preserve all other behavior and existing tests. Run node --test tests/clamp.test.cjs, then finish. Only src/clamp.cjs may change. Do not merge or deploy.", "acceptance_criteria": "Identify provider_unavailable and budget_exhausted with source IDs and verbatim causal evidence, excluding the healthy record. Independent validation must bind the profile, input and output digests." if analysis else "All seven existing regression tests pass. A separate verification Job and required acceptance-regression CI verify the exact PR head; evidence is downloadable in Deck.", "status": "ready", "priority": "urgent", "budget_minutes": 5 if analysis else 10, "budget": {"time_minutes": 5 if analysis else 10, "max_attempts": 1 if analysis else 2, "max_tokens": 20000 if analysis else 200000, "max_cost_minor": cost, "currency": "USD", "max_concurrency": 1, "authority": [profile["authority"]] if analysis else ["source.read", "source.write", "pull_request.create"]}}
            if args.mvp:
                catalog = api("/api/v1/operator/projects/prj_default/workloads")
                selected = next(p for p in catalog["profiles"] if p["id"] == ("diagnostic-analysis/v1" if analysis else "repository/v1"))
                body["budget"]["authority"] = selected["authority"]
                if args.exercise == "token-budget":
                    body["budget"]["max_tokens"] = 1
                invocation = {"profile_id": selected["id"], "profile_digest": selected["digest"], "task": body}
                key = "agent-mvp-" + str(time.time_ns())
                save(key + "-invocation.json", invocation)
                task = api("/api/v1/operator/projects/prj_default/workloads/invoke", invocation, key=key)
                replay = api("/api/v1/operator/projects/prj_default/workloads/invoke", invocation, key=key)
                assert task["id"] == replay["id"], "Duplicate invocation created work"
                save(task["id"] + "-invocation.json", {"key": key, "request": invocation, "replayed_task_id": replay["id"]})
            else:
                task = api("/api/v1/tasks", body)
            save(task["id"] + "-task.json", task)
            save(args.value + "-selected.json", {"task_id": task["id"]})
            emit({"task_id": task["id"], "kind": args.value, "cost_cap_usd": cost / 100})
            try:
                set_intake(True, task["id"])
                deadline = time.monotonic() + 45
                while detail(task["id"])["task"]["status"] == "ready":
                    assert time.monotonic() < deadline, "Task was not claimed"
                    time.sleep(1)
            finally:
                set_intake(False)
            deadline = time.monotonic() + 45
            while True:
                d = detail(task["id"])
                if d["task"]["status"] in TERMINAL:
                    raise RuntimeError("Task stopped before approval: " + d["task"]["status"])
                pending = [x for x in d["decisions"] if x["status"] == "pending"]
                if pending:
                    break
                assert time.monotonic() < deadline, "Plan was not produced"
                time.sleep(1)
            assert len(pending) == 1
            plan = d["runs"][0]["plan"]
            if analysis:
                assert profile["profile_digest"] in plan and "no repository or mutation authority" in plan
            else:
                assert "jmlero/marina-acceptance" in plan and "Allowed paths: src." in plan and "No merge or deployment is authorized" in plan
            if args.exercise in ("restart-marina-planning", "restart-deck-planning"):
                service = args.exercise.split("-")[1]
                kube("-n", "marina-system", "rollout", "restart", "deployment/" + service)
                kube("-n", "marina-system", "rollout", "status", "deployment/" + service, "--timeout=50s")
                after = detail(task["id"])
                assert after["runs"][0]["id"] == d["runs"][0]["id"]
                assert next(x for x in after["decisions"] if x["status"] == "pending")["id"] == pending[0]["id"]
                save(task["id"] + "-exercise.json", {"exercise": args.exercise, "run_id": d["runs"][0]["id"], "decision_id": pending[0]["id"], "preserved": True})
            answer = api("/api/v1/operator/decisions/" + pending[0]["id"] + "/answer", {"answer": "Approve plan"})
            save(task["id"] + "-approval.json", answer)
            emit({"task_id": task["id"], "approved": True, "intake_enabled": False})
            if args.exercise in ("restart-deck", "restart-marina", "cancel"):
                deadline = time.monotonic() + 55
                while True:
                    jobs = [j for j in resource("marina-workers", "jobs")["items"] if j["metadata"].get("labels", {}).get("marina.dev/attempt")]
                    pods = [p for p in resource("marina-workers", "pods")["items"] if p["metadata"].get("labels", {}).get("marina.dev/attempt")]
                    running = [p for p in pods if p["status"].get("phase") == "Running"]
                    for item in jobs + pods:
                        save(task["id"] + "-before-" + item["kind"] + "-" + item["metadata"]["uid"] + ".json", item)
                    if running:
                        break
                    assert time.monotonic() < deadline, "No active worker observed"
                    assert detail(task["id"])["task"]["status"] not in TERMINAL, "Work finished before exercise"
                    time.sleep(.25)
                for pod in pods:
                    try:
                        save(task["id"] + "-before-worker-log.txt", kube("-n", "marina-workers", "logs", pod["metadata"]["name"], "--tail=30"))
                    except RuntimeError:
                        pass
                before = [{"name": j["metadata"]["name"], "uid": j["metadata"]["uid"]} for j in jobs]
                if args.exercise == "cancel":
                    d = detail(task["id"])
                    response = api("/api/v1/operator/runs/" + d["runs"][0]["id"] + "/commands", {"type": "cancel", "reason": "MVP observed active worker cancellation"})
                else:
                    service = args.exercise.removeprefix("restart-")
                    kube("-n", "marina-system", "rollout", "restart", "deployment/" + service)
                    response = kube("-n", "marina-system", "rollout", "status", "deployment/" + service, "--timeout=50s")
                for pod in pods:
                    try:
                        save(task["id"] + "-after-worker-log.txt", kube("-n", "marina-workers", "logs", pod["metadata"]["name"], "--tail=30"))
                    except RuntimeError:
                        pass
                save(task["id"] + "-exercise.json", {"exercise": args.exercise, "jobs_before": before, "response": response,
                    "jobs_after": [{"name": j["metadata"]["name"], "uid": j["metadata"]["uid"]} for j in resource("marina-workers", "jobs")["items"]]})
                emit({"task_id": task["id"], "exercise": args.exercise, "jobs_before": before})
        elif args.action in ("watch", "result"):
            task_id = args.value
            assert task_id and task_id.startswith("tsk_")
            d = detail(task_id)
            assert d["task"]["title"].startswith(PREFIX)
            if args.action == "watch":
                deadline = time.monotonic() + (180 if args.mvp else 35)
                while True:
                    d = detail(task_id)
                    for kind in ("jobs", "pods"):
                        for item in resource("marina-workers", kind)["items"]:
                            save(task_id + "-" + kind + "-" + item["metadata"]["uid"] + ".json", item)
                            if kind == "pods":
                                try:
                                    logs = kube("-n", "marina-workers", "logs", item["metadata"]["name"], "--tail=30")
                                    if logs:
                                        save(task_id + "-worker-log.txt", logs)
                                except RuntimeError:
                                    pass
                    emit({"task_id": task_id, "status": d["task"]["status"], "latest": [{k: e.get(k) for k in ("type", "message")} for e in d["events"][:1]]})
                    if d["task"]["status"] in TERMINAL or time.monotonic() > deadline:
                        break
                    time.sleep(.25 if args.mvp else 3)
            else:
                downloads = []
                for evidence in d["evidence"]:
                    digest = evidence.get("artifact_digest")
                    if digest:
                        raw = api("/api/v1/operator/tasks/" + task_id + "/artifacts/" + digest, raw=True)
                        assert "sha256:" + hashlib.sha256(raw).hexdigest() == digest
                        value = json.loads(raw)
                        save(task_id + "-artifact-" + digest[7:] + ".json", value)
                        downloads.append({"digest": digest, "bytes": len(raw), "value": value})
                diagnostics = json.loads(kube("-n", "marina-system", "exec", "deployment/marina", "--", "/marina", "diagnostics", "--journal-path", "/data/marina.db"))
                executions = [e for e in diagnostics["executions"] if e["external_work_id"] == task_id]
                ids = {e["id"] for e in executions}
                attempts = [a for a in diagnostics["runtime_attempts"] if a["execution_id"] in ids]
                handles = {a["provider_handle"] for a in attempts}
                events = resource("marina-workers", "events")["items"]
                jobs = {}
                for event in events:
                    obj = event["involvedObject"]
                    handle = obj["name"] + "/" + obj["uid"]
                    if obj["kind"] == "Job" and "sha256:" + hashlib.sha256(handle.encode()).hexdigest() in handles:
                        jobs[obj["name"]] = obj
                events = [e for e in events if any(e["involvedObject"]["name"].startswith(n) for n in jobs)]
                result = {"task_id": task_id, "status": d["task"]["status"], "runs": d["runs"], "commands": d["commands"], "events": d["events"], "evidence": d["evidence"], "downloads": downloads, "executions": executions, "attempts": attempts,
                          "model_calls": [v for v in diagnostics.get("model_calls", []) if v["execution_id"] in ids],
                          "tool_decisions": [v for v in diagnostics.get("tool_decisions", []) if v["execution_id"] in ids],
                          "jobs": list(jobs.values()), "kubernetes_events": [{k: e.get(k) for k in ("involvedObject", "reason", "message", "firstTimestamp", "eventTime")} for e in events]}
                save(task_id + "-result.json", result)
                emit({"task_id": task_id, "status": result["status"], "attempts": len(attempts), "downloads": len(downloads), "model_calls": len(result["model_calls"]), "tool_decisions": len(result["tool_decisions"])})
    except Exception as error:
        emit({"error": type(error).__name__, "message": str(error)})
        raise SystemExit(1) from None


if __name__ == "__main__":
    main()
