# KINT-09/10 permanent local installation follow-up

- Date: 2026-09-26
- Environment: `kind-marina-local` on `podman-machine-default-root`, with Deck
  at `https://localhost:8443`
- Result: queued chat turn survived a Marina restart; a pinned Marina image
  upgrade and previous-image/data-copy rollback retained historical chat
  messages. Both backlog items remain **Open**.

## KINT-09: restart after invocation

With Marina task intake disabled and the prepared `kint16-chat-4` template
selected, a bounded Deck test-channel invocation created human message
`msg_b782c867e29ed37cc877` in thread `thr_714d3fc00f38590e1045`.
Marina's rollout restart was requested two seconds after the invocation. The
new Pod became Ready and the thread received one agent reply,
`msg_27e1545b19d5c0a2d043`. The copied Marina journal recorded one completed
turn, 601 used tokens, zero remaining reservation, and that exact published
message ID. SQLite `integrity_check` returned `ok`.

The restart request preceded this turn's journal creation. This establishes
queued-turn delivery across a Marina restart; it does **not** establish a
restart during a model call or the core repository-work restart row of the
[failure matrix](../plans/kagent-integration.md#mandatory-failure-matrix).

## KINT-10: retained copy, image upgrade and rollback

The protected local rollback directory, outside Git and outside temporary
storage, captured the private installation configuration and the five existing
PVC identities. The two PostgreSQL stores were dumped through their running
servers; RustFS was copied while no work was active. Deck and Marina were
stopped for their SQLite/artifact copies, both copied databases passed
`integrity_check`, and both Deployments returned Ready. The copy contains the
completed KINT-16 thread and the completed restart turn above. It is a local
rehearsal copy, not an atomic snapshot of work in progress.

| Stage | Observation |
| --- | --- |
| Previous Marina image | `localhost/the-platform/marina@sha256:c2b57de840e61651702d549252ecfa8e7ba9637b444d5647406b41ca5772fb2a` |
| Rehearsal image | Same source, built with `VERSION=kint10-rehearsal`, loaded into Kind and pinned to containerd manifest `sha256:82f2ade075b5bf68920d09756088cf7ccfa3646bb49691bc8203101289566fae` |
| After upgrade | Marina Ready; both earlier Deck threads retained their human and agent message IDs; prepared chat revision `60439797f69facaab287affb32a84d857c2ee6e1f7b69152be707c2ccb1a3c22` remained selected and Ready. |
| Rollback | Marina stopped; its upgraded data directory was retained separately on the Kind node; the saved Marina PVC copy and previous private image pin were restored. Marina and Deck returned Ready, Marina used the previous digest with `--intake-enabled=false`, and both threads and the prepared revision remained readable. |

The retained Marina copy contains completed turns
`msg_5813908d536ab4672ae9` and `msg_b782c867e29ed37cc877` with their
original published message IDs and 340/601 used tokens respectively. The
engine and Substrate PVC identities remained stable. The existing local
[installation guide](../../deploy/kubernetes/local-podman-kagent/README.md)
describes the private copy and rollback sequence.

The original pre-KINT-16 Kind API server manifest had been saved only under
`/private/tmp` and was removed by overnight temporary-file cleanup. The
protected rollback directory now holds the **current** manifest and a clearly
labeled reconstruction made by replacing its single
`--runtime-config=certificates.k8s.io/v1beta1=true` argument with the earlier
empty flag. The reconstruction is not a byte-verified original and was not
applied. Future installations must retain the actual original in persistent
private storage before changing that flag.

This rehearsed the release image/data mechanics with terminal chat history.
The image changes only the binary version string; it does not test a schema
migration or a different kagent/Substrate release. No in-flight kagent task
survived an upgrade or rollback in this exercise. KINT-10 still needs that
active-work proof and documented supported release versions. KINT-11 remains
gated on KINT-09 and KINT-10; no repository-work cutover was made.
