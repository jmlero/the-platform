# Workload API client guide

Deck remains the task ledger. Marina projects its installed profiles into Deck;
clients use Deck's operator session, CSRF protection and existing task/result APIs.
See [decision 0016](../../../docs/decisions/0016-workload-invocation-and-context-tool.md),
[OpenAPI](../../../deck/api/openapi.yaml) and the
[profile configuration](profiles-v1.md).

## Contract

All paths below start with `/api/v1`. Profile metadata contains no credentials.

| Operation | Method and path | Authorization and behavior |
| --- | --- | --- |
| Discover API capabilities | `GET /operator/provider` | Operator session; require `workload.discover` and `workload.invoke`. |
| Discover installed profiles | `GET /operator/projects/{project}/workloads` | Operator session; expired catalogs return an empty list. |
| Invoke selected profile | `POST /operator/projects/{project}/workloads/invoke` | Session, CSRF and required `Idempotency-Key`; returns one pinned Ready task. |
| Inspect progress/results | `GET /operator/tasks/{task}` | Session; task, runs, events, commands and evidence. |
| Inspect immutable inputs | `GET /operator/tasks/{task}/context` | Session; selected sources and recorded worker snapshot. |
| Cancel a run | `POST /operator/runs/{run}/commands` | Session and CSRF; `{"type":"cancel","reason":"Operator request"}`. Completion waits for observed worker termination. |
| Download evidence | `GET /operator/tasks/{task}/artifacts/{digest}` | Session; use a digest registered in the task's evidence. Deck authenticates the artifact fetch to Marina. |
| Refresh installed catalog | `PUT /projects/{project}/workloads` | Marina service credential with project scope and `workload.publish`; not an operator mutation. |

An empty catalog can mean no configured workload, no updated publisher or an
expired publication. Existing installations need the new Deck migration, Marina
binary and `workload.publish` service grant. The installation preparation script
adds that grant for newly prepared installations. Discovery and invocation do not
enable task intake, override approvals or increase installation ceilings.
Existing installations can use the offline
[two-workload upgrade procedure](../../../deploy/kubernetes/agent-platform/README.md).
The [combined local acceptance](../../../docs/validation/agent-11-13-2026-09-22.md)
records API invocations, real Jobs, outputs and failure/recovery boundaries.

Invocation requires the exact ID/digest returned by discovery. Copy the profile's
authority into an explicit task budget, choose positive `max_tokens` and
`time_minutes` within its ceilings, `max_concurrency: 1`, and enough attempts
(repository: two; analysis: one). Set a cost cap in the configured currency.
Context uses active project source revisions and explicitly selected message IDs
at authorization time; inspect the resulting task's `context` references.

`409 conflict` means stale/unavailable profile or reuse of an idempotency key with
different input. `422 invalid_input` means invalid criteria, authority or budget.
Retain the exact invocation JSON and its key before sending. Replay both unchanged
after a lost response, including after a profile withdrawal. A fresh discovery
must not silently replace the profile on a retry. Ordinary edits cannot replace
a task's pinned profile; authorize a new task after reviewing an upgrade.

## Minimal Python client

Save the following as `workload_client.py`. It uses only Python's standard library,
keeps sessions in memory, verifies TLS using the installation CA, and refuses
redirects. `DECK_TOKEN_FILE` names the private operator token file, not a Marina
service credential. `DECK_CA_FILE` names the CA certificate. The optional
`DECK_URL` defaults to the local installation `https://localhost:8443`.

```python
import http.cookiejar
import json
import os
from pathlib import Path
import ssl
import sys
import urllib.parse
import urllib.request

class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None

base = os.environ.get("DECK_URL", "https://localhost:8443").rstrip("/")
if urllib.parse.urlsplit(base).scheme != "https":
    raise SystemExit("Use the installation HTTPS endpoint")
jar = http.cookiejar.CookieJar()
client = urllib.request.build_opener(
    urllib.request.ProxyHandler({}), NoRedirect(),
    urllib.request.HTTPCookieProcessor(jar),
    urllib.request.HTTPSHandler(context=ssl.create_default_context(
        cafile=os.environ["DECK_CA_FILE"])),
)

def api(method, path, body=None, key=None, raw=False):
    headers = {"Content-Type": "application/json"}
    if method != "GET":
        for cookie in jar:
            if cookie.name == "deck_operator_csrf":
                headers["X-CSRF-Token"] = cookie.value
    if key:
        headers["Idempotency-Key"] = key
    request = urllib.request.Request(
        base + "/api/v1" + path, method=method, headers=headers,
        data=None if body is None else json.dumps(body).encode(),
    )
    with client.open(request, timeout=30) as response:
        data = response.read()
    return data if raw else (json.loads(data) if data else None)

def segment(value):
    return urllib.parse.quote(value, safe="")

action, *args = sys.argv[1:]
api("POST", "/operator/session", {
    "token": Path(os.environ["DECK_TOKEN_FILE"]).read_text().strip()
})
try:
    if action == "profiles":
        result = api("GET", "/operator/projects/" + segment(args[0]) + "/workloads")
    elif action == "capabilities":
        result = api("GET", "/operator/provider")
    elif action == "invoke":
        project, key, filename = args
        result = api("POST", "/operator/projects/" + segment(project) + "/workloads/invoke",
                     json.loads(Path(filename).read_text()), key=key)
    elif action == "task":
        result = api("GET", "/operator/tasks/" + segment(args[0]))
    elif action == "cancel":
        result = api("POST", "/operator/runs/" + segment(args[0]) + "/commands",
                     {"type": "cancel", "reason": "Operator API request"})
    elif action == "artifact":
        task, digest, filename = args
        data = api("GET", "/operator/tasks/" + segment(task) + "/artifacts/" + segment(digest), raw=True)
        with open(filename, "xb") as output:
            output.write(data)
        result = {"saved": filename}
    else:
        raise SystemExit("Use capabilities, profiles PROJECT, invoke PROJECT KEY JSON_FILE, task TASK, cancel RUN, or artifact TASK DIGEST OUTPUT")
    print(json.dumps(result, indent=2))
finally:
    api("DELETE", "/operator/session")
```

Discover before authorizing work:

```sh
python3 workload_client.py capabilities
python3 workload_client.py profiles prj_default
```

Prepare `invocation.json` with the discovered values and reviewed task budget:

```json
{
  "profile_id": "diagnostic-analysis/v1",
  "profile_digest": "<digest from discovery>",
  "task": {
    "title": "Analyze blocked diagnostic runs",
    "description": "Explain the selected diagnostic fixture.",
    "acceptance_criteria": "Return validated findings with source quotes.",
    "budget": {
      "time_minutes": 5,
      "max_attempts": 1,
      "max_tokens": 20000,
      "max_cost_minor": 10,
      "currency": "USD",
      "max_concurrency": 1,
      "authority": ["analysis.run.<digest from discovery>"]
    }
  }
}
```

Only invoke after reviewing the selected authority and budget. This example's
cost cap is USD 0.10, subject to nonzero installed model price rates and ceilings.
Keep the same key for retries of this exact request:

```sh
python3 workload_client.py invoke prj_default operator-analysis-001 invocation.json
python3 workload_client.py task TASK_ID
python3 workload_client.py cancel RUN_ID
python3 workload_client.py artifact TASK_ID ARTIFACT_DIGEST validation.json
```

Cancellation is an alternative when stopping work, not a prerequisite for result
retrieval. Poll task inspection for state and `commands` acknowledgment. The task
drawer exposes the same result evidence and progress. `model.usage` and
`execution.progress` events include confirmed tokens/cost plus pending calls and
reserved budgets; reservations must not be presented as confirmed spend.

Repository invocation uses `repository/v1`, its discovered digest and three
explicit source/PR grants. The result remains a verified PR ready for human
review. Analysis has no source-write/PR authority, and only v2 may use selected
project context and `read_context_source`. Both preserve the existing approval
policy. Pause/resume/replan/retry and automatic revision loops remain unsupported.

## Tool and diagnostics

The worker's only added HTTP tool is
`POST /workers/{attempt}/tools/read-context-source` on Marina, with its scoped
attempt Bearer credential and `{"uri":"<exact URI from frozen context>"}`.
Unknown arguments, non-selected sources, non-allowlisted tools and expired or
revoked authority fail closed. The harness cannot choose a destination or obtain
an upstream credential. The gateway records durable `tool.decision` events before
serving selected bytes. This is a worker protocol, not a client invocation API.

Use the existing offline `marina diagnostics --journal-path /path/to/marina.db`
command for correlated, redacted execution/attempt/model/tool/artifact records.
Unknown model usage has `usage_known: false` and null actual values. Context
snapshots and evidence can contain selected source content; diagnostics omit it.
The [validation report](../../../docs/validation/agent-07-10-2026-09-21.md)
distinguishes deterministic coverage from observed local network isolation.
