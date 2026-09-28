# Permanent local kagent and chat profile

This overlay extends the [local repository profile](../local-podman/README.md)
without selecting kagent for repository work. The local acceptance and current
limits are in [the KINT-16 report](../../../docs/validation/kint-16-2026-09-26.md).
Use only `kind-marina-local` on `podman-machine-default-root`; keep Marina new
task intake disabled outside a selected exercise.

## First installation

1. Save the Deck and Marina private configuration, Deployment manifests and PVC
   data. Retain the old image references. The previous release and data copy are
   the local rollback point in a protected directory outside Git and temporary
   storage.
2. Stage and verify the [pinned engine assets](../kagent-trial/README.md) in a
   private directory. Retain a copy of the original Kind API server manifest;
   this pinned Substrate release needs the certificate `v1beta1` API described in
   that guide. Keep the **original** API server manifest in the same persistent
   private rollback directory before changing its runtime flag. Install into
   unused namespaces with
   `python3 scripts/install-kagent-trial.py <assets> --helm <helm> --permanent`.
   It applies `the-platform.dev/installation: local-kagent` ownership labels.
   Close `kint-preparation-downloads` after the image and golden snapshot
   preparation. Do not rerun the first-install command over existing resources.
3. Build current Deck and Marina images, load them into Kind and pin the
   **containerd manifest digests** in the private Kustomization. `kind load`
   can import an archive under an `import-...` index digest that differs from
   the tagged image manifest. Check `ctr -n k8s.io images ls` in the Kind node
   and tag each local manifest as `<image>@sha256:<manifest>` with
   `ctr -n k8s.io images tag` before applying the digest-pinned overlay.
   `scripts/check-installation.py <installation>` must pass before apply.
4. Build `marina/Dockerfile.conversation-byo` for Linux ARM64. Push it to a
   registry reachable by the dedicated Kind worker, and pin the registry's OCI
   digest in a `kint-conversation-byo` Harness. Its image command is
   `/conversation-byo`; its Substrate worker pool is `kint-worker`. A Podman
   image ID is not the registry OCI digest.
5. Use this overlay as the private Kustomization resource. Keep its existing
   credentials and PVC patches. In the private workflow, select the
   `kint-conversation-byo` Harness without a template or revision, and set
   `budget_minutes`, `max_tokens`, and the fixed delegation limits from the
   [profile contract](../../../marina/docs/workloads/profiles-v1.md). Marina
   creates a `marina-` AgentTemplate, ModelConfig and same-name `gateway`
   Secret for every chat turn and repository attempt, and deletes them after
   the engine instance is gone. `Role/marina-engine-templates` grants exactly
   that in `kint-engine`; Marina refuses to adopt or delete resources without
   its `the-platform.dev/owner: marina` label.
6. Serve both harness images from the task-owned registry at `10.89.0.2:5000`,
   because a replaced worker keeps no image cache and atelet has no internet
   egress. Mirror the pinned native Codex image without changing its digest,
   for example with
   `skopeo copy --preserve-digests --dest-tls-verify=false docker://ghcr.io/kagent-dev/kagent/codex-harness@sha256:<digest> docker://10.89.0.2:5000/kagent-dev/codex-harness:<tag>`
   inside the Podman VM, and pin `10.89.0.2:5000/kagent-dev/codex-harness@sha256:<digest>`
   in the `kint-codex` Harness. The overlay's `atelet-local-registry` policy lets
   atelet reach that registry only. Without it a golden actor retries the pull
   and stays in `ACTOR_STATE_RESUMING`, holding a worker.
7. Allow `atenet-egress` to reach Marina over TLS: create its `marina-gateway-ca`
   Secret from the private installation `ca.crt`, mount it read-only at
   `/run/marina-ca` in `atenet-egress`, and set the dynamic HTTPS backend's
   `backendTLS.root` to `/run/marina-ca/ca.crt` in
   `atenet-egress-agentgateway-config`. Roll out the egress Deployment. The
   overlay permits only the needed kagent controller and Marina model route;
   it does not open direct worker internet access.
8. Apply the reviewed private Kustomization, roll out Marina, and verify Deck
   at `https://localhost:8443` and disabled Marina intake. A Deck invocation
   should yield one agent reply or an explicit blocker in the same thread, and
   no `the-platform.dev/owner=marina` resources should remain afterwards.

Each invocation gets its own template, so turns need no preparation between
them and Marina keeps running. Keep earlier terminal threads and engine history.

## Recovery and rollback

After a Podman VM restart, compare the worker's current Pod UID and IP with any
in-flight Substrate actor assignment before dispatch. The local KINT-16 test
found a stale actor route when the Pod kept its UID but received a new IP.
Reconcile or let an uncertain attempt block; do not resend it blindly. Once no
work is active, restart only the dedicated `kint-worker` Deployment to obtain a
fresh UID and address. This remains a KINT-09 failure case, not proof of
transparent recovery.

Each worker runs one actor. A golden actor stuck in `ACTOR_STATE_DELETING`,
with atelet logging `runsc state` exit status 128 on every retry, keeps its
worker's slot. While no work is active, deleting only that worker Pod releases
the slot; the actor is then reported crashed with no worker.

To roll back the platform release, disable new intake, reconcile active attempts,
restore the saved private workflow and Kustomization, previous Deck/Marina image
references and retained data copy, then verify both Deployments and historical
threads. Preserve the engine PVCs and exact old release artifacts through
rollback verification. An active attempt must never move to the legacy engine.

### Verified local image pair

The [active upgrade and rollback](../../../docs/validation/kint-10-recovery-2026-09-26.md)
used kagent `1.0.0-alpha1@850c464726881317997c32515f8b621b949d6ce1`,
Substrate `0.2.0-beta4@a7701e8dc8cb63a18d63e7fe771aff5a5ecca279`,
and these Marina Linux ARM64 manifests from the same source and journal schema:

| Role | Version | Local manifest digest |
| --- | --- | --- |
| Previous | `kint10-cycle-base-20260926` | `sha256:23e755b583e764d11cd855ec1c262dc8810dd6ff4327617180d523daf12ba858` |
| Candidate | `kint10-cycle-next-20260926` | `sha256:dbb103bf3f5887f0f619c09594b053c6f221b6e2620595846c2a46def733129a` |

These local digests are available in the dedicated Kind node; they are not
registry releases. The pair includes Marina's controller and gateway drain:
the current conversation cycle finishes before its HTTP listener closes, the
HTTP drain permits five minutes, and the Pod has a 330-second termination
grace. The [earlier failed attempt](../../../docs/validation/kint-10-active-2026-09-26.md)
shows why both the drain and exact digest import matter.

To repeat the local procedure, while no work is active, record the five PVC
UIDs and image digests with `kubectl --context kind-marina-local get pvc -A`
and the Deck/Marina Deployment images. Copy the private installation directory
into a mode-0700 directory outside Git and temporary storage. Take logical
`pg_dumpall` copies from `kint-engine-postgresql` and `ate-system/postgres-0`,
and copy RustFS. Scale Deck and Marina to zero, wait for their Pods to stop,
copy their complete PVC directories from `marina-local-control-plane`, check
both copied SQLite files with `PRAGMA integrity_check`, and scale both services
back to one Ready replica. Keep those copies and the previous image available.

Build and import the candidate archive into the dedicated Kind node using
`podman --connection podman-machine-default-root` and
`ctr -n k8s.io images import`. An imported tag alone does not satisfy a pinned
Pod image. Add and verify the exact
`localhost/the-platform/marina@sha256:<manifest-digest>` reference with
`ctr -n k8s.io images tag` and `ctr -n k8s.io images ls`. Select that digest
in the private Kustomization, run `scripts/check-installation.py`, apply with
the explicit `kind-marina-local` context, and wait for Marina Ready. Compare
the original Deck thread/message IDs, Marina usage and the prepared kagent
revision. The active upgrade test started a fresh chat turn first and changed
the image only after its model reservation was visible; its exact turn then
completed with one reply.

For the retained-data rollback, wait for active work to finish and take a new
protected copy that includes its terminal state. Stop Marina and wait for its
Pod to disappear. Move its current PVC directory aside on the Kind node,
restore the retained Marina PVC directory at the same path and ownership,
select the previous cached digest, validate/apply the private Kustomization,
and scale Marina back to one Ready replica. Confirm `PRAGMA integrity_check`,
the same Deck message IDs and model usage, no pending turns, and disabled
intake. The kagent, Substrate and Deck PVCs stay in place; do not replace their
identities. The rehearsal kept the upgraded Marina data directory separately.

This is the supported **local image pair** only. Schema-aware downgrade,
broader kagent/Substrate version changes, and automated backups remain outside
this pilot's acceptance scope.
