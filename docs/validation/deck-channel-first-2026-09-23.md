# Deck channel-first workspace — 2026-09-23

## Scope

Implements the manual Deck interaction and lifecycle portion of
[decision 0019](../decisions/0019-channel-first-project-work.md). The default is a
project conversation with Tasks and Context tabs, rather than a task board.
New projects create one channel plus one GitHub repository association request.
Existing channels, task identities and execution history are retained.

A message can become a Backlog draft without acceptance criteria. Selected
messages and originating thread identities remain attached. Ready authorizes
eligible execution within the existing installation constraints. The four human
stages are Backlog, Ready, In progress and Done. Verified results await review;
Done requires operator acceptance tied to a completed run. The v1 execution
status remains compatible. Dependencies are checked at discovery and direct claim;
cycles and cross-project edges are rejected. Code prerequisites need an explicit
operator attestation of base-branch availability as well as acceptance. This is
not automatic merge detection.

## Deterministic and real-process checks

Passed:

- `make check`: installation scripts, CLI, Deck backend/frontend and Marina.
- `make conformance`: durable threads against real Deck processes, CLI operator
  sessions, and Marina's real-Deck consumer suite, including both existing workflows.
- `make -C deck test check` after the acceptance-replay correction: Go tests/vet,
  27 frontend tests, and Svelte checks with no errors or warnings.
- `npm --prefix deck/web run build` for the embedded browser assets.
- `python3 deck/scripts/test-project-work.py`: project/channel creation replay,
  Backlog promotion before refinement, dependency-cycle rejection, ready-discovery
  and direct-claim enforcement, verified-result versus human acceptance, acceptance
  replay, dependent release, and persistence across a real Deck process restart.
  This script is also part of `make -C deck conformance`.
- Store tests cover code availability separately from human acceptance, stale
  versions, incomplete results, export/import, acceptance clearing on requeue,
  and preservation of prerequisites against draft deletion.

The real-process proof uses deterministic synthetic evidence in isolated temporary
stores. It does not establish live model execution, Git merge observation, or
conversational agent behavior.

## Browser verification

Chrome was exercised against an isolated local Deck process at
`http://127.0.0.1:8187`, with a separate test database and no workers:

1. Created `example/channel-first-preview` with one channel and a pending
   repository association; no GitHub request or credential was created.
2. Posted a discussion, opened a persistent thread and replied.
3. Selected the original message as context and promoted the reply to a Backlog
   draft with no acceptance criteria; the draft opened successfully in its drawer.
4. Closed the drawer and observed the linked Backlog task in the original thread.
5. Saved a message as a project note and verified that Context showed a versioned
   document labelled Reference data with the source message identity. Approval as
   guidance remained an explicit operator control.
6. Inspected the layout visually: project sidebar, readable conversation, persistent
   composer, secondary task/context tabs and collapsed execution diagnostics.

These tests use synthetic local content only. Existing user results were not
accepted or modified to demonstrate the feature.

## Local installation

Environment: existing `kind-marina-local` on
`podman-machine-default-root`, with Deck at `https://localhost:8443`.
The Mac remains the host. No GCP or production deployment was performed.

The final local image is:

```text
localhost/the-platform/deck@sha256:452fa8dc678d19e5f1c5ecdceb04e0976e5c2b816cb3802520d11995632411ea
```

The update changes Deck's image and its private installation pin, preserving its
PVC, credentials, no-sign-in preference and service configuration. Marina's
configuration and intake selection were not changed by this work. The update
checks that no active task exists immediately before replacing Deck. The first
rollout encountered delayed Calico cleanup and a missing containerd digest alias;
registering the imported image under its exact digest resolved image lookup.

Chrome confirmed the live Context Lens project opens on Conversation, with the
existing channels and project-scoped Tasks/Context tabs. API inspection retained
at least the 54 tasks present before the first update (55 after concurrent work),
and no historical result was automatically accepted. The final rollout verified
all 55 pre-update task identities were retained and completed successfully.
`/healthz` reported `ok`.

## Remaining boundary

The final live composer offers Save only and truthfully reports that no
conversation agent is configured.
Thread selection/invocation records alone do not execute a conversational agent.
A Marina/kagent conversation worker, automatic Backlog drafting, automatic factual
summary/note maintenance, clarification and result return remain separate CHAT and
LEAN-03 work. This evidence does not mark those capabilities complete, enable
additional project runtimes, change KINT cutover gates, or grant merge/deploy
permission.
