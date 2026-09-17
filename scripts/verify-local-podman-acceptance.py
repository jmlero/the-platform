#!/usr/bin/env python3
"""Exercise offline fixture checks and selected network boundaries with real Jobs.

Run prepare-local-podman-egress.py and apply its policy and Deployment patch first. Uses no provider
credentials, model inference, Git mutation, or task execution authority.
"""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time

KUBECTL = ["kubectl", "--context", "kind-marina-local", "--request-timeout=15s"]
PROBE = r"""
const fs = require('node:fs');
const net = require('node:net');
const https = require('node:https');
const dns = require('node:dns').promises;
const {spawnSync} = require('node:child_process');
const role = process.env.PROBE_ROLE;
function assert(ok, message) { if (!ok) throw Error(message); }
async function tcp(host, port, expected) {
  const reachable = await new Promise(resolve => {
    const socket = net.createConnection({host, port});
    let done = false;
    function finish(ok) { if (!done) { done = true; socket.destroy(); resolve(ok); } }
    socket.setTimeout(2500, () => finish(false));
    socket.on('error', () => finish(false));
    socket.on('connect', () => finish(true));
  });
  assert(reachable === expected, `${host}:${port} expected=${expected} actual=${reachable}`);
  console.log(`PASS ${role} ${host}:${port} ${reachable ? 'reachable' : 'blocked'}`);
}
async function tls(url, internal=false) {
  console.log(`DNS ${new URL(url).hostname}: ${JSON.stringify(await dns.lookup(new URL(url).hostname,{all:true}))}`);
  await new Promise((resolve, reject) => {
    const request = https.get(url, {headers: {'User-Agent': 'Marina-local-acceptance'},
      ...(internal ? {ca: fs.readFileSync('/run/ca/ca.crt')} : {})}, response => {
      response.destroy();
      const status = response.statusCode;
      if (internal ? status === 200 : status >= 200 && status < 500) {
        console.log(`PASS ${role} trusted TLS ${url} HTTP ${status}`); resolve();
      } else reject(Error(`unexpected HTTP status ${status}`));
    });
    request.on('error', error => reject(Error(`${url}: ${error.message}`)));
    request.setTimeout(5000, () => request.destroy(Error('TLS timeout')));
  });
}
(async () => {
  assert(!fs.existsSync('/var/run/secrets/kubernetes.io/serviceaccount/token'), 'unexpected API token');
  if (role === 'baseline') {
    for (const host of ['api.github.com','github.com','openrouter.ai','ghcr.io','1.1.1.1','kubernetes.default.svc'])
      await tcp(host,443,true);
  } else if (role === 'controller') {
    await tls('https://deck.marina-system.svc:8080/readyz',true);
    await tcp('kubernetes.default.svc',443,true);
    for (const url of ['https://api.github.com/meta','https://github.com','https://openrouter.ai/api/v1/models'])
      await tls(url);
    for (const host of ['ghcr.io','1.1.1.1']) await tcp(host,443,false);
  } else {
    await tls('https://marina.marina-system.svc:8090/healthz',true);
    for (const host of ['api.github.com','github.com','openrouter.ai','ghcr.io','1.1.1.1','kubernetes.default.svc'])
      await tcp(host,443,false);
    await tcp('deck.marina-system.svc',8080,false);
    fs.mkdirSync('/work/fixture/src',{recursive:true});
    fs.mkdirSync('/work/fixture/tests',{recursive:true});
    for (const path of ['src/clamp.cjs','tests/clamp.test.cjs'])
      fs.copyFileSync('/fixture/'+path,'/work/fixture/'+path);
    const check = () => spawnSync('node',['--test','tests/clamp.test.cjs'],{cwd:'/work/fixture',encoding:'utf8',timeout:10000});
    const broken=check();
    assert(broken.status === 1 && broken.stdout.includes('# fail 4'), 'fixture must expose four boundary failures');
    console.log('PASS offline original fixture: 3 passed, 4 expected boundary failures');
    const path='/work/fixture/src/clamp.cjs';
    fs.writeFileSync(path,fs.readFileSync(path,'utf8').replace('maximum - 1','maximum'));
    const repaired=check();
    assert(repaired.status === 0 && repaired.stdout.includes('# pass 7'), 'repaired fixture must pass all seven checks');
    console.log('PASS offline repaired fixture: 7 passed; no package downloads');
  }
})().catch(error => { console.error(error.message); process.exitCode=1; });
"""


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("image", help="locally loaded worker reference pinned with @sha256")
    args = parser.parse_args()
    if not re.fullmatch(r"[^\s]+@sha256:[a-f0-9]{64}", args.image):
        parser.error("an immutable worker image digest is required")
    fixture = Path(__file__).resolve().parents[1] / "deploy/kubernetes/first-release/acceptance-repository"
    deployment = json.loads(subprocess.check_output(KUBECTL + ["-n", "marina-system", "get", "deployment", "marina", "-o", "json"]))
    aliases = deployment["spec"]["template"]["spec"].get("hostAliases", [])
    if not aliases:
        parser.error("apply the paired marina-destinations.json Deployment patch first")
    prefix = f"acceptance-{time.time_ns()}"
    for role, namespace, label in [("baseline", "marina-system", "acceptance-probe"),
                                   ("controller", "marina-system", "marina"),
                                   ("worker", "marina-workers", "marina-worker")]:
        name = f"{prefix}-{role}"
        config = {"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": name, "namespace": namespace},
                  "data": {"probe.cjs": PROBE, "clamp.cjs": (fixture / "src/clamp.cjs").read_text(),
                           "clamp.test.cjs": (fixture / "tests/clamp.test.cjs").read_text()}}
        container = {"name": "probe", "image": args.image, "imagePullPolicy": "Never",
                     "command": ["node", "/fixture/probe.cjs"], "env": [{"name": "PROBE_ROLE", "value": role}],
                     "resources": {"requests": {"cpu": "100m", "memory": "128Mi"},
                                   "limits": {"cpu": "1", "memory": "512Mi", "ephemeral-storage": "128Mi"}},
                     "securityContext": {"allowPrivilegeEscalation": False, "readOnlyRootFilesystem": True,
                                         "capabilities": {"drop": ["ALL"]}},
                     # A controller-labelled probe must never become a Marina Service endpoint.
                     "readinessProbe": {"exec": {"command": ["/bin/false"]}, "periodSeconds": 1},
                     "volumeMounts": [{"name": "fixture", "mountPath": "/fixture", "readOnly": True},
                                      {"name": "ca", "mountPath": "/run/ca", "readOnly": True},
                                      {"name": "work", "mountPath": "/work"}]}
        pod = {"restartPolicy": "Never", "automountServiceAccountToken": False, "enableServiceLinks": False,
               "securityContext": {"runAsNonRoot": True, "runAsUser": 65532, "runAsGroup": 65532,
                                   "fsGroup": 65532, "seccompProfile": {"type": "RuntimeDefault"}},
               "containers": [container], "volumes": [
                   {"name": "work", "emptyDir": {"sizeLimit": "64Mi"}},
                   {"name": "ca", "configMap": {"name": "marina-worker-ca"}},
                   {"name": "fixture", "configMap": {"name": name, "items": [
                       {"key": "probe.cjs", "path": "probe.cjs"},
                       {"key": "clamp.cjs", "path": "src/clamp.cjs"},
                       {"key": "clamp.test.cjs", "path": "tests/clamp.test.cjs"}]}}]}
        if role == "controller":
            pod["hostAliases"] = aliases
        job = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": name, "namespace": namespace},
               "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 90, "ttlSecondsAfterFinished": 300,
                        "template": {"metadata": {"labels": {"app.kubernetes.io/name": label}}, "spec": pod}}}
        command = KUBECTL + ["-n", namespace]
        try:
            for resource in [config, job]:
                subprocess.run(command + ["create", "-f", "-"], input=json.dumps(resource), text=True, check=True)
            deadline = time.monotonic() + 105
            while True:
                status = json.loads(subprocess.check_output(command + ["get", "job", name, "-o", "json"]))["status"]
                if status.get("succeeded") or status.get("failed") or time.monotonic() > deadline:
                    subprocess.run(command + ["logs", f"job/{name}"], check=False)
                    if not status.get("succeeded"):
                        raise SystemExit(f"{role} probe failed")
                    break
                time.sleep(1)
        finally:
            # Delete only the unique resources created by this invocation.
            subprocess.run(command + ["delete", "job,configmap", name, "--ignore-not-found", "--wait=false"], check=True)
    print("PASS local fixture and selected network boundary checks; live workflow acceptance remains separate.")


if __name__ == "__main__":
    main()
