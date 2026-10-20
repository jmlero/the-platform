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

Deck publishes the catalog from the Marina Workloads mapped in its projects file
([decision 0033](../../../docs/decisions/0033-marina-platform-api.md)). An empty
catalog means no mapped Workload or an expired publication. Discovery and invocation do not
enable task intake, override approvals or increase installation ceilings.
Existing installations can use the offline
[native-engine upgrade procedure](../../../deploy/kubernetes/agent-platform/README.md).
The [combined local acceptance](../../../docs/validation/agent-11-13-2026-09-22.md)
records API invocations, real Jobs, outputs and failure/recovery boundaries.

Invocation requires the exact ID/digest returned by discovery. Copy the profile's
authority into an explicit task budget, choose positive `max_tokens` and
`time_minutes` within its ceilings, `max_concurrency: 1`, and enough attempts
(repository: at least three). Use seven starts for one full repair or eleven for
two, subject to the original model/time/cost limits. Set a cost cap in the
configured currency with nonzero producer and reviewer rates.
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
  "profile_id": "repository/v2",
  "profile_digest": "<digest from discovery>",
  "task": {
    "title": "Fix the inclusive range boundary",
    "description": "Fix the boundary check in src and add a regression in tests.",
    "acceptance_criteria": "The trusted checks and required CI pass on the exact landed commit.",
    "budget": {
      "time_minutes": 5,
      "max_attempts": 3,
      "max_tokens": 20000,
      "max_cost_minor": 10,
      "currency": "USD",
      "max_concurrency": 1,
      "authority": ["source.read", "source.write", "source.fast_forward"]
    }
  }
}
```

Only invoke after reviewing the selected authority and budget. This example's
cost cap is USD 0.10, subject to nonzero installed model price rates and ceilings.
Keep the same key for retries of this exact request:

```sh
python3 workload_client.py invoke prj_default operator-repository-001 invocation.json
python3 workload_client.py task TASK_ID
python3 workload_client.py cancel RUN_ID
python3 workload_client.py artifact TASK_ID ARTIFACT_DIGEST validation.json
```

Cancellation is an alternative when stopping work, not a prerequisite for result
retrieval. Poll task inspection for state and `commands` acknowledgment. The task
drawer exposes the same result evidence and progress. `model.usage` and
`execution.progress` events include confirmed tokens/cost plus pending calls and
reserved budgets; reservations must not be presented as confirmed spend.

Repository invocation uses the discovered digest for `repository/v1` (PR delivery)
or `repository/v2` (guarded fast-forward delivery). Each requires three explicit
grants: source read/write plus PR creation or fast-forward, respectively. The
landing profile waits for required CI on the exact task-branch commit before
updating the unchanged base branch. Both repository profiles preserve the installed
approval policy. Pause/resume/replan/retry and automatic revision loops remain
unsupported. The retired diagnostic profile cannot be invoked.

## Questions and diagnostics

Conversation invocations use the configured agent and originating Deck thread.
A pending question records its expiry. Exact answer replay returns the original
human message; a changed answer conflicts. Expired or terminal-turn pending
questions reject new answers. Marina routes an accepted answer to the same
recorded waiting engine task. Uncertain delivery does not trigger a blind resend.

Use the existing offline `marina diagnostics --journal-path /path/to/marina.db`
command for correlated, redacted execution/attempt/model/tool/artifact records.
Unknown model usage has `usage_known: false` and null actual values. Context
snapshots and evidence can contain selected source content; diagnostics omit it.
The [validation report](../../../docs/validation/agent-07-10-2026-09-21.md)
distinguishes deterministic coverage from observed local network isolation.
