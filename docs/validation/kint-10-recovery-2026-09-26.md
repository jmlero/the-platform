# KINT-10 local active upgrade and retained-data rollback

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, Deck at
  `https://localhost:8443`; no GCP resources were added.
- Result: the pinned local release pair passed an upgrade during an active
  kagent chat model call and a previous-image, retained-data-copy rollback.
  This closes KINT-10 for the local pilot. It does not cover a schema-aware
  downgrade or a repository-work restart; those have separate gates.

The earlier [active transition attempts](kint-10-active-2026-09-26.md)
retained intent and usage but ended in deadline blockers. An identical
100-item prompt without a rollout completed in about 15 seconds, confirming
the prompt itself could finish. Marina originally cancelled the active
conversation controller on SIGTERM and closed its HTTP listener before that
controller finished. The fix lets the current bounded cycle finish before
closing the gateway, uses a five-minute HTTP drain and a 330-second Pod
termination grace. Focused tests hold an HTTP response open past the old
10-second limit, keep the gateway available until its controller finishes, and
verify that SIGTERM does not cancel the active conversation cycle.

## Pinned local versions

| Component | Tested version or image |
| --- | --- |
| kagent | `1.0.0-alpha1` at commit `850c464726881317997c32515f8b621b949d6ce1` |
| Substrate | `0.2.0-beta4` at commit `a7701e8dc8cb63a18d63e7fe771aff5a5ecca279` |
| Marina previous | Same-source release `kint10-cycle-base-20260926`, local containerd manifest `sha256:23e755b583e764d11cd855ec1c262dc8810dd6ff4327617180d523daf12ba858` |
| Marina candidate | Same-source release `kint10-cycle-next-20260926`, local containerd manifest `sha256:dbb103bf3f5887f0f619c09594b053c6f221b6e2620595846c2a46def733129a` |

Both Marina images include the controller and gateway drain fix and use the
same journal schema. Each archive was imported to the dedicated Kind node and
given its exact digest reference before deployment. This pair validates the
local release mechanics; it makes no claim about a migration to a different
kagent/Substrate or database schema version.

## Active upgrade

After the previous image and 330-second termination grace were Ready, a fresh
single-turn chat template started invocation
`msg_503ef7e1f576837bf81e` in thread `thr_3c573939d474160b5538`.
Marina had reserved 6,428 tokens for the real model call when the candidate
image was applied. The candidate Pod became Ready. The original turn then
completed, used 1,445 tokens with zero remaining reservation, and published
exactly one substantive Deck agent message,
`msg_d756873c37d7f3d6e5a9`. Its kagent engine attempt succeeded, and its
artifacts were collected and instance deleted. The Marina journal had no
active conversation turns and passed SQLite `integrity_check`; the earlier
successful chat threads and pinned template revisions remained readable.

## Retained copy and rollback

With no active work, a protected private directory outside Git retained the
current installation configuration, five PVC identities, PostgreSQL logical
dumps, RustFS data, and cold Deck/Marina SQLite and artifact copies. Both cold
SQLite copies passed `integrity_check`. The Marina Pod was stopped; its
post-upgrade data directory was moved aside without deleting it, the retained
Marina PVC copy was restored, and the previous digest was selected. Marina and
Deck returned Ready with task intake disabled. The restored journal still
recorded the same completed turn, 1,445 used tokens, zero outstanding
reservation and the exact published message ID. Deck still showed the single
agent reply and the two earlier historical threads. The kagent and Substrate
PVC identities were not replaced. A fresh unused `kint16-chat-11` slot was
prepared after rollback.

The [local installation guide](../../deploy/kubernetes/local-podman-kagent/README.md)
records the supported pair, digest-import check, private copy and rollback
sequence. The prior release and upgraded data directory remain available for
inspection; the test did not merge, deploy outside local Kind, or change
repository-task intake.

`make -C marina check`, `make check-installation`, root `make check` and
`make conformance` passed. The latter exercises real Deck processes with
deterministic Git/runtime boundaries; the active image, model, kagent and
rollback observations above came from the local Kind installation.
