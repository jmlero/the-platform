# Pinned kagent runtime adapter

The adapter implements `runtime.Adapter` using the public
`kagent.api.v1alpha1.AgentInstanceService` and A2A v1 gRPC APIs from the
[locked source](../../../deploy/kubernetes/kagent-trial/lock.json).
The [gateway](../workerapi/README.md) implements governed admission and access.
Repository implementation uses native Codex; conversation turns use the BYO
actor. Kubernetes Jobs run independent verification only. Supported behavior and
acceptance evidence belong in [current state](../../../docs/status.md).

The caller supplies a `grpc.ClientConnInterface`, its installed
endpoint identity, the SQLite journal, the existing artifact store and a required
`Admission` implementation (`workerapi.Server` for governed work). Each
runtime specification must select the backend/version, endpoint, namespace,
harness/template and exact prepared revision, plus profile and context digests.
The digest-addressed artifact named by `ContextURI` contains the initial message;
`ContextDigest` separately binds the frozen selected context package. This is the
lifecycle transport boundary. Repository result requirements remain owned by
the workflow and independent verifier.
The adapter calls admission before instance creation and message delivery,
including recovery. The gateway rechecks calls and owns source access and budget
reservations. A missing admission fails closed; this adapter alone cannot grant
a task authority.

Migration `012_engine_attempts.sql` adds one correlation record per existing
runtime attempt. It retains the specification digest, message ID, instance ID,
prepared revision, route, context/task IDs, phase, deletion/collection intent and
observed state. Updates use version guards and reject changed bound identities.
Old runtime specifications omit `engine`, preserving their serialized digests.

| Durable phase | Next operation |
| --- | --- |
| Creating, no instance ID | Replay `CreateAgentInstance` with the recorded request key. |
| Instance bound, ready | Verify its complete identity and exact revision; persist send intent, then send once. |
| Sending, no task ID | Inspect stored task history for the recorded message ID. Never resend. |
| Active | Get the bound task, follow its committed stream and retain its normalized observation. |
| Revision mismatch | Block without sending; retain the instance for explicit cancellation. |
| Cancellation/deletion requested | Delete the exact instance, then observe `NotFound` before acknowledging termination. |
| Deleted | Return the retained observation; never recreate, even if the upstream key was deleted. |

The pinned creation RPC does not accept a requested prepared revision. A changed
template can create an idle instance at another revision, but the adapter blocks
before delivering any task and records that instance for cleanup. Instance
creation and message delivery uncertainty are separate. An interrupted send
with no discoverable stored task is a truthful blocker, including a crash just
before the send. Task/history lookup is bounded to ten pages of 100 tasks and
4 MiB per response. Missing/changed identities also block.

Both cancellation modes use the synchronous instance deletion API; this includes
Substrate suspension and deletion. A task's `CANCELED` state alone is insufficient.
One typed input-required question retains the original task identity. The exact
answer has a durable message ID and digest; ambiguous delivery is inspected,
never resent. Deck rejects changed replays, expired questions and questions on
terminal turns. Unsupported authentication requests block. General operator
resume remains unsupported.

Successful/failed task outputs are collected before normal cleanup. The adapter
stores bounded A2A artifact envelopes in Marina's existing content-addressed
store and runtime artifact journal. It never fetches engine-supplied URLs and
does not treat these envelopes as workflow evidence. The workflow owns result
translation and independent validation. Cancellation may discard uncollected
outputs; cleanup is repeatable and previously collected artifacts survive it.

RPC operations are bounded by a 60-second timeout. Stream observation drains at
most 64 events for five seconds. Three consecutive unavailable observations stop
the original instance without replacement work; normal observation timeouts do
not count as unavailable compute. Cleanup failures remain durable and retryable.
Upstream error bodies are
not exposed in adapter errors. The upstream module requires Go 1.27; its
Substrate module replacement is repeated explicitly in `marina/go.mod` because
Go does not inherit dependency replacements. No kagent internal package or
engine database is imported or read.

Tests:

```sh
cd marina
go test ./internal/kagent ./internal/runtime ./internal/journal/sqlite
```

The lifecycle tests use an explicit synthetic admission double. The opt-in
`TestLiveIsolatedLifecycle` uses that same double and the
[isolated trial](../../../deploy/kubernetes/kagent-trial/README.md), with
preparation egress closed and a loopback Kubernetes port-forward. Set
`KINT03_ENDPOINT=127.0.0.1:18083` and `KINT03_REVISION` to the fixture's observed
`latestSuccessfulRevision`, then run:

```sh
go test -v ./internal/kagent -run '^TestLiveIsolatedLifecycle$' -count=1
```

It drops successful real create/delete responses and interrupts message delivery
before admission, so it dispatches no model task. Accepted-message recovery,
artifacts, changed revisions and other failure paths also have deterministic gRPC
coverage with real SQLite. See the [initial lifecycle evidence](../../../docs/validation/kint-03-2026-09-22.md)
and [final removal and failure acceptance](../../../docs/validation/kint-12-17-2026-10-01.md).
