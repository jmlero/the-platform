#!/usr/bin/env python3
"""Install, check or remove the Marina platform on a local Kind cluster.

  up      create the cluster and install every component (safe to rerun)
  verify  prove it works: a Marina Turn, your own agent in a sandbox, policy
  down    delete the cluster and its registry (private files are kept)

Works with Podman or Docker. See deploy/kubernetes/marina-local/README.md.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
HERE = ROOT / "deploy/kubernetes/marina-local"
LOCK = json.loads((ROOT / "deploy/kubernetes/kagent-trial/lock.json").read_text())
ENGINE = "1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1"
CALICO = ("https://raw.githubusercontent.com/projectcalico/calico/v3.32.2/manifests/calico.yaml",
          "a8c828a06a87c629a282ebbc424895b77f3a030251993e41ea400a743675bb02")
GATEWAY_API = ("https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.6.1/standard-install.yaml",
               "24d931f22abd8e40c973264319ead7cfa09d0fb7716b7ab1ee2ff174cb063a73")
REGISTRY_IMAGE = "docker.io/library/registry@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"
CODEX_HARNESS = "ghcr.io/kagent-dev/kagent/codex-harness@sha256:91f69cef4d02e30c078f866167a103853c4446f315e2aa59e6c4db8696881c85"
# Budget rates in micro-USD per million tokens. Unknown models get high rates,
# so budgets stop work early rather than late; set your own in workflow.json.
RATES = {"openai/gpt-5-mini": (250000, 2000000), "openai/gpt-6-luna": (100000, 500000)}
UNKNOWN_RATES = (5000000, 20000000)


def workflow(model, github):
    """Marina's installed profile: chat on the fake model, a real model through
    agentgateway, and repository work when a GitHub repository is given."""
    if github and not model:
        raise ValueError("GitHub mode needs a real model (--model-key-file)")

    def model_profile(name):
        if not name:
            return {"endpoint": "https://fake-model.platform-gateway.svc/v1", "name": "fake/model",
                    "token_file": "/run/marina-secrets/agentgateway-client-key", "max_output_tokens": 4096,
                    "input_micros_per_million": 0, "output_micros_per_million": 0, "currency": "USD"}
        rates = RATES.get(name, UNKNOWN_RATES)
        return {"endpoint": "https://agentgateway.platform-gateway.svc/v1", "name": name,
                "token_file": "/run/marina-secrets/agentgateway-client-key", "max_output_tokens": 4096,
                "input_micros_per_million": rates[0], "output_micros_per_million": rates[1], "currency": "USD"}

    engine = {"backend": "kagent", "version": ENGINE, "endpoint": "kint-engine-controller.kint-engine.svc:8083",
              "namespace": "kint-engine"}
    config = {
        "version": 1, "project_id": "demo.hello", "policy_version": "marina-local-v1",
        "public_url": "https://marina.marina-system.svc:8090",
        "tls_cert_file": "/run/marina-tls/tls.crt", "tls_key_file": "/run/marina-tls/tls.key",
        "signing_key_file": "/run/marina-secrets/worker-signing-key",
        "artifact_directory": "/data/artifacts", "artifact_token_file": "/run/artifacts/token",
        "kubernetes": {"server_url": "https://kubernetes.default.svc", "namespace": "marina-workers",
                       "token_file": "/var/run/secrets/kubernetes.io/serviceaccount/token",
                       "ca_file": "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt",
                       # Chat-only mode runs no verification Job; the image is never pulled.
                       "image": (github or {}).get("verifier") or "localhost:5000/the-platform/marina-verifier@sha256:" + "0" * 64,
                       "service_account": "marina-worker", "worker_ca_config_map": "marina-worker-ca"},
        "conversation": {"engine": dict(engine, harness="kint-conversation-byo"), "budget_minutes": 5, "max_tokens": 20000},
        "git": {}, "model": model_profile(model),
        "allowed_paths": ["."], "checks": [], "required_ci": [], "require_approval": True,
        "max_tokens": 200000, "budget_minutes": 30,
    }
    if github:
        reviewer = "openai/gpt-6-luna" if model != "openai/gpt-6-luna" else "openai/gpt-5-mini"
        config.update({
            "git": {"api_url": "https://api.github.com", "repository": github["repository"],
                    "clone_url": "https://github.com/" + github["repository"] + ".git",
                    "token_file": "/run/marina-secrets/git-token", "base_branch": "main",
                    "author_name": "Marina", "author_email": "marina@localhost"},
            "repository_engine": dict(engine, harness="kint-codex"),
            "repository_engine_gateway_url": "https://marina.marina-system.svc",
            "reviewer_model": model_profile(reviewer),
            "checks": [{"id": "check-%d" % (i + 1), "directory": ".", "command": ["sh", "-c", c]}
                       for i, c in enumerate(github["checks"])],
            "allowed_paths": github["paths"],
        })
    return config


def hosts_toml(host):
    return '[host."http://%s"]\n  capabilities = ["pull", "resolve"]\n' % host


class Local:
    def __init__(self, args):
        self.args = args
        self.name = args.name
        self.context = "kind-" + self.name
        self.node = self.name + "-control-plane"
        self.registry = self.name + "-registry"
        self.state = Path(args.state or "~/.local/share/the-platform/clusters/" + self.name).expanduser()
        self.cache = self.state / "downloads"
        self.cli = args.provider
        self.helm = args.helm or shutil.which("helm")
        self.env = dict(os.environ, KIND_EXPERIMENTAL_PROVIDER=self.cli,
                        KIND_EXPERIMENTAL_PODMAN_NETWORK=self.name, KIND_EXPERIMENTAL_DOCKER_NETWORK=self.name)

    # --- helpers -----------------------------------------------------------
    def run(self, *command, input=None, check=True, quiet=False):
        result = subprocess.run([str(c) for c in command], input=input, text=True, env=self.env,
                                capture_output=True)
        if check and result.returncode:
            raise SystemExit("failed: %s\n%s" % (" ".join(map(str, command[:6])), result.stderr.strip()[-2000:]))
        if not quiet and result.stdout.strip():
            print(result.stdout.strip())
        return result.stdout

    def k(self, *command, **kw):
        return self.run("kubectl", "--context", self.context, *command, **kw)

    def apply(self, text):
        self.k("apply", "-f", "-", input=text, quiet=True)

    def secret(self, namespace, name, *files, kind="generic"):
        rendered = self.k("-n", namespace, "create", "secret", kind, name, *files, "--dry-run=client", "-o", "json", quiet=True)
        self.apply(rendered)

    def configmap(self, namespace, name, *files):
        self.apply(self.k("-n", namespace, "create", "configmap", name, *files, "--dry-run=client", "-o", "json", quiet=True))

    def rollout(self, namespace, kind="deploy", name=None):
        target = "%s/%s" % (kind, name) if name else kind
        self.k("-n", namespace, "rollout", "status", target, "--timeout=300s", quiet=True)

    def step(self, text):
        print("==> " + text, flush=True)

    def download(self, url, sha256):
        path = self.cache / url.rsplit("/", 1)[1]
        if not path.exists():
            self.cache.mkdir(parents=True, exist_ok=True)
            urllib.request.urlretrieve(url, path)
        if hashlib.sha256(path.read_bytes()).hexdigest() != sha256:
            path.unlink()
            raise SystemExit("checksum mismatch: " + url)
        return path

    def node_ip(self):
        return self.k("get", "node", self.node, "-o",
                      "jsonpath={.status.addresses[?(@.type=='InternalIP')].address}", quiet=True).strip()

    def exists(self, *command):
        return subprocess.run(["kubectl", "--context", self.context, *command], capture_output=True).returncode == 0

    def push(self, local, name):
        """Push to the registry and return the manifest digest the cluster sees."""
        target = "localhost:5001/" + name
        flags = ["--tls-verify=false"] if self.cli == "podman" else []
        self.run(self.cli, "tag", local, target, quiet=True)
        self.run(self.cli, "push", *flags, target, quiet=True)
        headers = self.run(self.cli, "exec", self.node, "curl", "-sfI",
                           "-H", "Accept: application/vnd.oci.image.manifest.v1+json",
                           "-H", "Accept: application/vnd.docker.distribution.manifest.v2+json",
                           "-H", "Accept: application/vnd.oci.image.index.v1+json",
                           "http://localhost:5000/v2/%s/manifests/%s" % tuple(name.split(":")), quiet=True)
        digest = [l.split(":", 1)[1].strip() for l in headers.splitlines() if l.lower().startswith("docker-content-digest")]
        return digest[0]

    def build(self, tag, dockerfile, context, *extra):
        self.step("Building " + tag)
        self.run(self.cli, "build", "-q", "-f", ROOT / dockerfile, "-t", tag, *extra, ROOT / context, quiet=True)

    def cert(self, name, *names):
        """A leaf certificate signed by the private installation CA."""
        if (self.state / (name + ".crt")).exists():
            return
        ext = self.state / (name + ".ext")
        ext.write_text("subjectAltName=%s\nextendedKeyUsage=serverAuth\n" % ",".join("DNS:" + n for n in names))
        self.run("openssl", "req", "-newkey", "rsa:3072", "-nodes", "-keyout", self.state / (name + ".key"),
                 "-out", self.state / (name + ".csr"), "-subj", "/CN=" + names[0], quiet=True)
        self.run("openssl", "x509", "-req", "-in", self.state / (name + ".csr"), "-CA", self.state / "ca.crt",
                 "-CAkey", self.state / "ca.key", "-CAcreateserial", "-out", self.state / (name + ".crt"),
                 "-days", "365", "-sha256", "-extfile", ext, quiet=True)
        for leftover in (ext, self.state / (name + ".csr")):
            leftover.unlink()

    # --- up ----------------------------------------------------------------
    def up(self):
        for tool in ("kind", "kubectl", "openssl", "ruby", self.cli):
            if not shutil.which(tool):
                raise SystemExit("missing tool: " + tool)
        if not self.helm:
            raise SystemExit("missing tool: helm (or pass --helm)")
        os.umask(0o077)
        self.state.mkdir(parents=True, exist_ok=True)
        self.credentials()
        self.cluster()
        self.images()
        self.engine()
        self.platform()
        self.marina()
        self.demo()
        self.harden()
        print("\nReady. Marina API https://marina.localhost:9443, kagent UI https://kagent.localhost:9443, "
              "Jaeger https://jaeger.localhost:9443 (CA: %s/ca.crt).\nNext: python3 scripts/marina-local.py verify"
              % self.state)

    def credentials(self):
        self.step("Private files in " + str(self.state))
        s = self.state
        if not (s / "ca.crt").exists():
            self.run("openssl", "req", "-x509", "-newkey", "rsa:3072", "-nodes", "-days", "365",
                     "-keyout", s / "ca.key", "-out", s / "ca.crt", "-subj", "/CN=Marina local CA",
                     "-addext", "basicConstraints=critical,CA:TRUE", "-addext", "keyUsage=critical,keyCertSign,cRLSign", quiet=True)
        self.cert("marina", "marina.marina-system.svc", "marina.marina-system.svc.cluster.local")
        self.cert("agentgateway", "agentgateway.platform-gateway.svc", "agentgateway.platform-gateway.svc.cluster.local")
        self.cert("fake-model", "fake-model.platform-gateway.svc", "fake-model.platform-gateway.svc.cluster.local")
        self.cert("ingress", "marina.localhost", "kagent.localhost", "jaeger.localhost")
        # agentgateway compares keys byte for byte: no trailing newline.
        for name in ("artifact-token", "worker-signing-key", "agentgateway-client-key", "agentgateway-agents-key"):
            if not (s / name).exists():
                (s / name).write_text(os.urandom(32).hex())
        for name, source in (("model-token", self.args.model_key_file), ("git-token", self.args.github_token_file)):
            value = Path(source).expanduser().read_text().strip() if source else ""
            (s / name).write_text(value)

    def cluster(self):
        clusters = self.run("kind", "get", "clusters", quiet=True).split()
        if self.name not in clusters:
            self.step("Creating Kind cluster " + self.name)
            current = subprocess.run(["kubectl", "config", "current-context"], capture_output=True, text=True).stdout.strip()
            self.run("kind", "create", "cluster", "--name", self.name, "--config", HERE / "kind.yaml", quiet=True)
            if current:  # kind switches the default context; keep the user's.
                self.run("kubectl", "config", "use-context", current, quiet=True)
        self.step("Calico network policy")
        self.k("apply", "--server-side", "-f", self.download(*CALICO), quiet=True)
        self.rollout("kube-system", "ds", "calico-node")
        self.step("Local registry")
        running = self.run(self.cli, "ps", "-a", "--filter", "name=^" + self.registry + "$", "--format", "{{.Names}}", quiet=True)
        if not running.strip():
            self.run(self.cli, "run", "-d", "--name", self.registry, "--network", "container:" + self.node,
                     "-v", self.registry + ":/var/lib/registry", REGISTRY_IMAGE, quiet=True)
        self.run(self.cli, "start", self.registry, quiet=True)
        for host in ("localhost:5000", self.node_ip() + ":5000"):
            self.run(self.cli, "exec", "-i", self.node, "sh", "-c",
                     "mkdir -p /etc/containerd/certs.d/%s && cat > /etc/containerd/certs.d/%s/hosts.toml" % (host, host),
                     input=hosts_toml(host), quiet=True)

    def images(self):
        self.build("localhost/the-platform/marina:local", "marina/Dockerfile", "marina")
        self.build("localhost/the-platform/marina-conversation-byo:local", "marina/Dockerfile.conversation-byo", "marina")
        self.digests = {"marina": self.push("localhost/the-platform/marina:local", "the-platform/marina:local"),
                        "conversation": self.push("localhost/the-platform/marina-conversation-byo:local",
                                                  "the-platform/marina-conversation-byo:local")}
        # Sandboxes start from this pause image. Served locally, so preparing a
        # sandbox never needs the internet after setup (for example after a restart).
        pause = next(i["pinned"] for i in LOCK["images"] if "/pause@" in i["pinned"])
        self.run(self.cli, "pull", "--platform", "linux/arm64", pause, quiet=True)
        self.digests["pause"] = self.push(pause, "kubernetes/pause:pinned")
        if self.args.github_repo:
            self.build("localhost/the-platform/marina-verifier:local", "marina/Dockerfile.verifier", ".",
                       "--ignorefile", ROOT / "marina/Dockerfile.verifier.dockerignore", "--target", "verifier")
            self.digests["verifier"] = self.push("localhost/the-platform/marina-verifier:local", "the-platform/marina-verifier:local")
            self.step("Mirroring the pinned Codex harness")
            self.run(self.cli, "pull", "--platform", "linux/arm64", CODEX_HARNESS, quiet=True)
            self.digests["codex"] = self.push(CODEX_HARNESS, "kagent-dev/codex-harness:pinned")

    def engine(self):
        self.step("kagent and Agent Substrate (gVisor sandboxes)")
        assets = self.cache / "engine"
        assets.mkdir(parents=True, exist_ok=True)
        for chart in LOCK["charts"]:
            if not (assets / chart["archive"]).exists():
                self.run(self.helm, "pull", chart["url"], "--version", chart["version"], "-d", assets, quiet=True)
        machine = {"Darwin": "darwin", "Linux": "linux"}[os.uname().sysname] + "-" + {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64"}[os.uname().machine]
        releases = {"kubectl-ate": ("kagent-dev/substrate", "v0.2.0-beta4", "kubectl-ate-" + machine),
                    "kagent-cli": ("kagent-dev/kagent", "v1.0.0-alpha1", "kagent-" + machine)}
        for name, (repo, tag, asset) in releases.items():
            path = assets / name
            if not path.exists():
                urllib.request.urlretrieve("https://github.com/%s/releases/download/%s/%s" % (repo, tag, asset), path)
                path.chmod(0o700)
        node_ip = self.node_ip()
        if not self.exists("get", "namespace", "ate-system"):
            self.run(sys.executable, ROOT / "scripts/install-kagent-trial.py", assets, "--helm", self.helm,
                     "--permanent", "--context", self.context, "--node", self.node, "--node-ip", node_ip, quiet=True)
        else:
            # The node can get a new address after a restart; keep the engine's policies on it.
            spec = importlib.util.spec_from_file_location("install_kagent", ROOT / "scripts/install-kagent-trial.py")
            installer = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(installer)
            policies = json.loads((ROOT / "deploy/kubernetes/kagent-trial/network-policy.json").read_text())["items"]
            installer.prepare_permanent([], policies)
            installer.pin_node(policies, node_ip)
            self.apply(json.dumps({"apiVersion": "v1", "kind": "List", "items": policies}))
        for namespace in ("ate-system", "kint-engine", "podcertificate-controller-system"):
            self.rollout(namespace, "deploy")
        self.rollout("ate-system", "ds", "atelet")
        # The sandbox node agent pulls agent images from the local registry.
        self.apply(json.dumps({"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
                               "metadata": {"name": "atelet-local-registry", "namespace": "ate-system"},
                               "spec": {"podSelector": {"matchLabels": {"app": "atelet"}}, "policyTypes": ["Egress"],
                                        "egress": [{"to": [{"ipBlock": {"cidr": node_ip + "/32"}}],
                                                    "ports": [{"protocol": "TCP", "port": 5000}]}]}}))
        self.k("patch", "sandboxconfig", "gvisor-default", "--type=merge", "-p", json.dumps(
            {"spec": {"pauseImage": "%s:5000/kubernetes/pause@%s" % (node_ip, self.digests["pause"])}}), quiet=True)
        self.k("apply", "-f", HERE / "engine/egress-to-models.yaml", quiet=True)
        # The egress proxy trusts the installation CA for Marina and model services.
        self.secret("ate-system", "marina-gateway-ca", "--from-file=ca.crt=%s" % (self.state / "ca.crt"))
        config = json.loads(self.k("-n", "ate-system", "get", "cm", "atenet-egress-agentgateway-config", "-o", "json", quiet=True))
        text = config["data"]["config.yaml"]
        if "/run/marina-ca/ca.crt" not in text:
            config["data"]["config.yaml"] = text.replace("backendTLS: {}", "backendTLS: {root: /run/marina-ca/ca.crt}", 1)
            self.apply(json.dumps(config))
            self.k("-n", "ate-system", "patch", "deploy", "atenet-egress", "--type=json", "-p", json.dumps([
                {"op": "add", "path": "/spec/template/spec/volumes/-",
                 "value": {"name": "marina-ca", "secret": {"secretName": "marina-gateway-ca"}}},
                {"op": "add", "path": "/spec/template/spec/containers/0/volumeMounts/-",
                 "value": {"name": "marina-ca", "mountPath": "/run/marina-ca", "readOnly": True}}]), quiet=True)
            self.rollout("ate-system", "deploy", "atenet-egress")

    def platform(self):
        s = self.state
        for namespace in ("marina-system", "platform-gateway"):
            if not self.exists("get", "namespace", namespace):
                self.k("create", "namespace", namespace, quiet=True)
        self.step("Kyverno admission policy")
        self.run(self.helm, "repo", "add", "kyverno", "https://kyverno.github.io/kyverno/", "--force-update", quiet=True)
        self.run(self.helm, "--kube-context", self.context, "upgrade", "--install", "kyverno", "kyverno/kyverno",
                 "--version", "3.9.1", "-n", "kyverno", "--create-namespace",
                 "-f", ROOT / "deploy/kubernetes/platform/kyverno/values.yaml", "--wait", "--timeout", "10m", quiet=True)
        self.k("apply", "-k", ROOT / "deploy/kubernetes/platform/kyverno/policies/audit", quiet=True)
        self.step("OpenTelemetry Collector and Jaeger")
        self.k("apply", "-k", ROOT / "deploy/kubernetes/platform/observability", quiet=True)
        self.step("agentgateway and the fake model")
        self.secret("platform-gateway", "agentgateway-tls", "--cert=%s" % (s / "agentgateway.crt"), "--key=%s" % (s / "agentgateway.key"), kind="tls")
        self.secret("platform-gateway", "agentgateway-provider", "--from-file=api-key=%s" % (s / "model-token"))
        self.secret("platform-gateway", "agentgateway-clients", "--from-file=marina=%s" % (s / "agentgateway-client-key"),
                    "--from-file=agents=%s" % (s / "agentgateway-agents-key"))
        self.k("apply", "-k", HERE / "gateway", quiet=True)
        self.secret("platform-gateway", "fake-model-tls", "--cert=%s" % (s / "fake-model.crt"), "--key=%s" % (s / "fake-model.key"), kind="tls")
        self.k("apply", "-f", HERE / "fake-model/fake-model.yaml", quiet=True)
        self.step("Gateway API and kgateway ingress")
        self.k("apply", "--server-side", "-f", self.download(*GATEWAY_API), quiet=True)
        self.run(self.helm, "--kube-context", self.context, "upgrade", "--install", "kgateway-crds",
                 "oci://cr.kgateway.dev/kgateway-dev/charts/kgateway-crds", "--version", "v2.4.5",
                 "-n", "kgateway-system", "--create-namespace", "--wait", quiet=True)
        self.run(self.helm, "--kube-context", self.context, "upgrade", "--install", "kgateway",
                 "oci://cr.kgateway.dev/kgateway-dev/charts/kgateway", "--version", "v2.4.5", "-n", "kgateway-system",
                 "-f", ROOT / "deploy/kubernetes/platform/ingress/values.yaml", "--wait", quiet=True)
        self.k("apply", "-k", HERE / "ingress", quiet=True)
        self.secret("platform-ingress", "ingress-tls", "--cert=%s" % (s / "ingress.crt"), "--key=%s" % (s / "ingress.key"), kind="tls")
        for namespace in ("platform-gateway", "platform-observability", "platform-ingress"):
            self.rollout(namespace)

    def marina(self):
        s = self.state
        github = None
        if self.args.github_repo:
            github = {"repository": self.args.github_repo, "checks": self.args.check, "paths": self.args.allowed_path,
                      "verifier": "localhost:5000/the-platform/marina-verifier@" + self.digests["verifier"]}
        model = self.args.model if self.args.model_key_file else None
        self.config = workflow(model, github)
        (s / "workflow.json").write_text(json.dumps(self.config, indent=2) + "\n")
        self.step("Marina (Helm chart)")
        self.secret("marina-system", "marina-credentials", *("--from-file=%s" % (s / n) for n in
                    ("worker-signing-key", "agentgateway-client-key", "git-token")))
        self.secret("marina-system", "marina-artifact-auth", "--from-file=token=%s" % (s / "artifact-token"))
        self.secret("marina-system", "marina-worker-tls", "--cert=%s" % (s / "marina.crt"), "--key=%s" % (s / "marina.key"), kind="tls")
        self.configmap("marina-system", "marina-worker-ca", "--from-file=ca.crt=%s" % (s / "ca.crt"))
        self.configmap("marina-system", "marina-workflow", "--from-file=workflow.json=%s" % (s / "workflow.json"))
        # Marina reaches the Kubernetes API on the node, after the Service's DNAT.
        self.apply(json.dumps({"apiVersion": "networking.k8s.io/v1", "kind": "NetworkPolicy",
                               "metadata": {"name": "marina-kubernetes-api", "namespace": "marina-system"},
                               "spec": {"podSelector": {"matchLabels": {"app.kubernetes.io/name": "marina"}}, "policyTypes": ["Egress"],
                                        "egress": [{"to": [{"ipBlock": {"cidr": self.node_ip() + "/32"}}],
                                                    "ports": [{"protocol": "TCP", "port": 6443}]}]}}))
        self.run(self.helm, "--kube-context", self.context, "upgrade", "--install", "marina", ROOT / "marina/deploy/helm/marina",
                 "-n", "marina-system", "-f", HERE / "marina-values.yaml", "--set", "image.digest=" + self.digests["marina"], quiet=True)
        self.configmap("marina-workers", "marina-worker-ca", "--from-file=ca.crt=%s" % (s / "ca.crt"))
        # A changed workflow ConfigMap does not restart Marina by itself.
        self.k("-n", "marina-system", "rollout", "restart", "deploy/marina", quiet=True)
        self.rollout("marina-system", "deploy", "marina")
        self.step("Engine harnesses for Marina")
        node_ip = self.node_ip()
        harnesses = [("kint-conversation-byo", {"byo": {}, "workload": {
            "image": "%s:5000/the-platform/marina-conversation-byo@%s" % (node_ip, self.digests["conversation"]),
            "command": ["/conversation-byo"]}})]
        if github:
            harnesses.append(("kint-codex", {"codex": {}, "workload": {
                "image": "%s:5000/kagent-dev/codex-harness@%s" % (node_ip, self.digests["codex"])}}))
        for name, spec in harnesses:
            spec.update({"substrate": {"workerPoolRef": {"name": "kint-worker"}, "snapshotPolicy": {"location": "s3://ate-snapshots/" + name}},
                         "allowedAgentTemplates": {"selector": {"matchLabels": {"kagent.dev/harness": name}}}})
            self.apply(json.dumps({"apiVersion": "kagent.dev/v1alpha3", "kind": "Harness",
                                   "metadata": {"name": name, "namespace": "kint-engine"}, "spec": spec}))

    def demo(self):
        self.step("Demo project: namespace demo, Project hello, Workload chat")
        # Without --github-repo the Project stays "Binding": chat needs no repository.
        repository = self.args.github_repo or "example/hello"
        config = self.config
        model = config["model"]["name"]
        documents = [
            {"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": "demo"}},
            {"apiVersion": "v1", "kind": "ServiceAccount", "metadata": {"name": "operator", "namespace": "demo"}},
            {"apiVersion": "rbac.authorization.k8s.io/v1", "kind": "RoleBinding", "metadata": {"name": "operator", "namespace": "demo"},
             "roleRef": {"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "marina-admin"},
             "subjects": [{"kind": "ServiceAccount", "name": "operator", "namespace": "demo"}]},
            {"apiVersion": "marina.dev/v1", "kind": "Project", "metadata": {"name": "hello", "namespace": "demo"},
             "spec": {"displayName": "Hello", "defaultWorkload": "chat",
                      "repository": {"provider": "github", "url": "https://github.com/%s.git" % repository, "baseBranch": "main",
                                     "credentialSecret": {"name": "marina-credentials", "key": "git-token"}}}},
            {"apiVersion": "marina.dev/v1", "kind": "Workload", "metadata": {"name": "chat", "namespace": "demo"},
             "spec": {"type": "conversation", "conversation": {"model": model, "repositoryTools": bool(self.args.github_repo),
                                                               "limits": {"maxTimeMinutes": 5, "maxTokens": 20000}}}},
        ]
        if self.args.github_repo:
            documents.append({"apiVersion": "marina.dev/v1", "kind": "Workload", "metadata": {"name": "change", "namespace": "demo"},
                              "spec": {"type": "repository-change", "repositoryChange": {
                                  "model": model, "allowedPaths": config["allowed_paths"], "planApproval": "required",
                                  "checks": config["checks"],
                                  "review": {"enabled": True, "model": config["reviewer_model"]["name"]},
                                  "delivery": {"mode": "pull-request"}}}})
        self.apply(json.dumps({"apiVersion": "v1", "kind": "List", "items": documents}))

    def harden(self):
        self.step("Enforcing admission policy and closing setup downloads")
        self.k("apply", "-k", ROOT / "deploy/kubernetes/platform/kyverno/policies/enforce", quiet=True)
        self.k("-n", "ate-system", "delete", "networkpolicy", "kint-preparation-downloads", "--ignore-not-found", quiet=True)

    # --- verify ------------------------------------------------------------
    def verify(self):
        ok = True

        def check(label, passed, detail=""):
            nonlocal ok
            ok = ok and passed
            print("%s %s%s" % ("PASS" if passed else "FAIL", label, (": " + detail) if detail else ""), flush=True)

        ca = str(self.state / "ca.crt")
        for host, path in (("marina", "/readyz"), ("kagent", "/"), ("jaeger", "/")):
            code = subprocess.run(["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "--cacert", ca,
                                   "https://%s.localhost:9443%s" % (host, path)], capture_output=True, text=True).stdout
            check("ingress https://%s.localhost:9443" % host, code == "200", code)

        name = "verify-%d" % int(time.time())
        self.apply(json.dumps({"apiVersion": "marina.dev/v1", "kind": "Turn", "metadata": {"name": name, "namespace": "demo"},
                               "spec": {"project": "hello", "workload": "chat", "thread": {"id": "thr_" + name, "messages": [
                                   {"id": "msg_1", "role": "human", "body": "Say hello in one sentence."}]},
                                        "budget": {"timeMinutes": 5, "maxTokens": 20000}}}))
        status = {}
        for _ in range(60):
            status = json.loads(self.k("-n", "demo", "get", "turn", name, "-o", "jsonpath={.status}", quiet=True) or "{}")
            if status.get("phase") in ("Succeeded", "Failed"):
                break
            time.sleep(5)
        check("Marina Turn through kagent and a gVisor sandbox", status.get("phase") == "Succeeded",
              (status.get("reply") or {}).get("answer", status.get("reason", "timed out")))

        self.step("Your own agent: building and deploying examples/hello-agent")
        example = HERE / "examples/hello-agent"
        self.run(self.cli, "build", "-q", "-t", "localhost/hello-agent:local", example, quiet=True)
        image = "%s:5000/hello-agent@%s" % (self.node_ip(), self.push("localhost/hello-agent:local", "hello-agent:local"))
        self.apply((example / "agent.yaml").read_text().replace("REGISTRY_IMAGE", image))
        for _ in range(60):
            ready = self.k("-n", "kint-engine", "get", "agenttemplate", "hello-agent", "-o",
                           "jsonpath={.status.harnesses[0].conditions[?(@.type=='Ready')].status}", quiet=True)
            if ready == "True":
                break
            time.sleep(5)
        cli = self.cache / "engine/kagent-cli"
        forward = subprocess.Popen(["kubectl", "--context", self.context, "-n", "kint-engine", "port-forward", "--address",
                                    "127.0.0.1", "svc/kint-engine-controller", "18083:8083"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        try:
            time.sleep(3)
            api = ["--api-url", "http://127.0.0.1:18083", "--gateway-url", "http://127.0.0.1:18083", "-n", "kint-engine"]
            created = json.loads(self.run(cli, *api, "-o", "json", "create", "agent-instance", "--harness", "hello-agent",
                                          "--agent-template", "hello-agent", "--request-id", name, quiet=True))
            answer = self.run(cli, *api, "invoke", "--agent-instance", created["agentInstance"]["id"],
                              "--task", "Say hello to the platform team.", check=False, quiet=True).strip()
            check("your own agent in a gVisor sandbox", bool(answer) and "rror" not in answer, answer[-200:])
            self.run(cli, *api, "delete", "agent-instance", created["agentInstance"]["id"], check=False, quiet=True)
        finally:
            forward.terminate()

        denied = subprocess.run(["kubectl", "--context", self.context, "apply", "--dry-run=server", "-f", "-"], text=True, capture_output=True,
                                input=json.dumps({"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "unpinned", "namespace": "marina-system"},
                                                  "spec": {"containers": [{"name": "c", "image": "busybox:latest"}]}}))
        check("Kyverno rejects an unpinned image in marina-system", denied.returncode != 0 and "digest" in denied.stderr)
        if not ok:
            raise SystemExit(1)

    # --- down --------------------------------------------------------------
    def down(self):
        # The registry shares the node's network, so it goes first.
        self.run(self.cli, "rm", "-f", self.registry, check=False, quiet=True)
        self.run("kind", "delete", "cluster", "--name", self.name)
        print("Removed %s. Private files stay in %s." % (self.name, self.state))


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("command", choices=["up", "verify", "down"])
    parser.add_argument("--name", default="marina", help="Kind cluster name (default marina)")
    parser.add_argument("--provider", choices=["podman", "docker"],
                        default=os.environ.get("KIND_EXPERIMENTAL_PROVIDER") or ("podman" if shutil.which("podman") else "docker"))
    parser.add_argument("--state", help="private files (default ~/.local/share/the-platform/clusters/NAME)")
    parser.add_argument("--helm", help="Helm 3 binary (default: helm on PATH)")
    parser.add_argument("--model-key-file", help="OpenRouter API key file; without it Marina uses the fake model")
    parser.add_argument("--model", default="openai/gpt-5-mini", help="OpenRouter model for Marina")
    parser.add_argument("--github-repo", help="owner/name: enables repository work (pull requests)")
    parser.add_argument("--github-token-file", help="fine-grained GitHub token file for --github-repo")
    parser.add_argument("--check", action="append", help="shell command Marina runs to verify a change (repeatable)")
    parser.add_argument("--allowed-path", action="append", help="repository folder agents may change (repeatable)")
    args = parser.parse_args()
    if args.github_repo and not (args.github_token_file and args.model_key_file and args.check and args.allowed_path):
        parser.error("--github-repo needs --github-token-file, --model-key-file, --check and --allowed-path")
    getattr(Local(args), args.command)()


if __name__ == "__main__":
    main()
