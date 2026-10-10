#!/usr/bin/env python3
"""Run disposable baseline and isolated worker checks in kind-marina-local."""
import argparse
import json
import subprocess
import time

kubectl = ["kubectl", "--context", "kind-marina-local"]
probe = r"""
const fs = require('fs');
const net = require('net');
const https = require('https');
async function connects(host, port) {
  return new Promise(resolve => {
    const socket = net.createConnection({host, port});
    let done = false;
    function finish(ok) { if (!done) { done = true; socket.destroy(); resolve(ok); } }
    socket.setTimeout(3000, () => finish(false));
    socket.on('error', () => finish(false));
    socket.on('connect', () => finish(true));
  });
}
(async () => {
  const {chromium} = require('/work/deck-web/node_modules/playwright');
  const browser = await chromium.launch({headless: true});
  try {
    const page = await browser.newPage();
    await page.setContent('<title>offline verifier</title><p>prepackaged browser</p>');
    if (await page.title() !== 'offline verifier') throw Error('browser DOM check failed');
    console.log(`PASS offline Chromium ${browser.version()}`);
  } finally { await browser.close(); }
  const isolated = process.env.ISOLATED === 'true';
  if (fs.existsSync('/var/run/secrets/kubernetes.io/serviceaccount/token')) {
    throw Error('unexpected Kubernetes credential');
  }
  if (isolated) {
    await new Promise((resolve, reject) => {
      const req = https.get('https://marina.marina-system.svc:8090/healthz',
        {ca: fs.readFileSync('/run/ca/ca.crt')}, response => {
          response.resume();
          response.statusCode === 200 ? resolve() : reject(Error('gateway not healthy'));
        });
      req.on('error', reject);
      req.setTimeout(5000, () => req.destroy(Error('gateway timeout')));
    });
    console.log('PASS trusted TLS connection to Marina');
  }
  for (const host of ['kubernetes.default.svc', '1.1.1.1']) {
    const reachable = await connects(host, 443);
    if (reachable === isolated) throw Error(`unexpected reachability: ${host} = ${reachable}`);
    console.log(`PASS ${host}: ${reachable ? 'baseline reachable' : 'worker blocked'}`);
  }
})().catch(error => { console.error(error.message); process.exitCode = 1; });
"""

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="localhost/the-platform/marina-verifier:local",
                        help="already imported verifier image tag or digest")
    args = parser.parse_args()
    image = args.image
    for namespace, isolated in (("marina-system", False), ("marina-workers", True)):
        name = f"local-network-check-{time.time_ns()}"
        pod = {
            "restartPolicy": "Never", "automountServiceAccountToken": False,
            "enableServiceLinks": False,
            "securityContext": {"runAsNonRoot": True, "runAsUser": 65532,
                                "runAsGroup": 65532, "fsGroup": 65532,
                                "seccompProfile": {"type": "RuntimeDefault"}},
            "containers": [{"name": "check", "image": image, "imagePullPolicy": "Never",
                            "command": ["/bin/sh", "-ec"],
                            "args": ["! command -v codex\ngo version\ngit --version\n"
                             "mkdir -p /work/deck-web\ncp /opt/marina-cache/deck-web/package*.json /work/deck-web/\n"
                             "cd /work/deck-web\nnpm ci --offline\nnode - <<'JS'\n" + probe + "\nJS"],
                            "env": [{"name": "ISOLATED", "value": str(isolated).lower()}],
                            "resources": {"requests": {"cpu": "100m", "memory": "128Mi"},
                                          "limits": {"cpu": "1", "memory": "512Mi", "ephemeral-storage": "1Gi"}},
                            "securityContext": {"allowPrivilegeEscalation": False,
                                                "readOnlyRootFilesystem": True, "capabilities": {"drop": ["ALL"]}},
                            "volumeMounts": [{"name": "ca", "mountPath": "/run/ca", "readOnly": True},
                                             {"name": "work", "mountPath": "/work"},
                                             {"name": "tmp", "mountPath": "/tmp"}]}],
            "volumes": [{"name": "ca", "configMap": {"name": "marina-worker-ca"}},
                        {"name": "work", "emptyDir": {"sizeLimit": "512Mi"}},
                        {"name": "tmp", "emptyDir": {"sizeLimit": "64Mi"}}],
        }
        job = {"apiVersion": "batch/v1", "kind": "Job", "metadata": {"name": name, "namespace": namespace},
               "spec": {"backoffLimit": 0, "activeDeadlineSeconds": 90, "ttlSecondsAfterFinished": 120,
                        "template": {"metadata": {"labels": {"app.kubernetes.io/name": "marina-worker"}}, "spec": pod}}}
        subprocess.run(kubectl + ["create", "-f", "-"], input=json.dumps(job), text=True, check=True)
        result = subprocess.run(kubectl + ["-n", namespace, "wait", "--for=condition=complete", f"job/{name}", "--timeout=60s"])
        subprocess.run(kubectl + ["-n", namespace, "logs", f"job/{name}"], check=True)
        if result.returncode:
            raise SystemExit("Network validation failed; inspect the temporary Job before its TTL expires")
    print("Local offline verifier tools/browser, TLS, absent API credential and enforced network isolation verified.")


if __name__ == "__main__":
    main()
