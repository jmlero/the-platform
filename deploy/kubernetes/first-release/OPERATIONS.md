# First-release recovery and maintenance

Use the existing installation, identities and volumes. For local work, every
command must select `kind-marina-local`; the namespace is `marina-system` and
workers are in `marina-workers`. The retained Deck claim is `appdeck-data`.
Never delete a PVC, recreate the cluster, or edit a ledger row to recover work.
Backups and restore drills remain deferred under decision 0010.

## Inspect before intervening

```sh
kubectl --context kind-marina-local -n marina-system get pods,pvc,deployments
kubectl --context kind-marina-local -n marina-workers get jobs,pods
kubectl --context kind-marina-local -n marina-system get events --sort-by=.lastTimestamp
kubectl --context kind-marina-local -n marina-system exec deployment/marina -- /marina doctor
python3 scripts/check-installation.py ~/.local/share/the-platform/marina-local/installation
```

Use Deck's task detail to record the task/run ID, state, pending command,
current PR head and evidence. Marina's `diagnostics --journal-path /data/marina.db`
exports redacted operational metadata. Keep raw logs and configuration private;
do not paste Secrets or rendered manifests into reports.

To stop **new** work, keep `--intake-enabled=false` in the private overlay's
Marina args and apply that overlay. Existing work continues to reconcile. Cancel
active work through Deck when maintenance requires stopping it; wait for command
acknowledgement and observed worker termination. Do not interpret a disabled
intake flag, failed readiness probe, or stopped controller as cancellation.

## Recovery cases

| Symptom | Detect and contain | Recover and verify |
| --- | --- | --- |
| Disk full / artifact write failure | Deck cannot obtain evidence, readiness/journal operations fail, or storage writes report no space. Stop new intake. Preserve both SQLite files, their WAL/SHM files and all referenced artifacts. | Expand the affected volume through the installation's storage provider, or remove only unrelated operator-owned files. Once enough free space exists to operate safely, use the retention procedure below for expired unreferenced blobs. Restart only the affected service and verify the same task/run/agent IDs, readiness and evidence downloads. If the storage class cannot expand, keep the run blocked pending storage work; do not delete evidence or databases to make room. |
| Deck unavailable | Check Deck readiness, TLS, auth policy and storage. Marina must not acquire new work or act after authority expires. | Restore Deck, then check Marina readiness, the exact existing run/fence and ordered projections. Expired or terminal work follows its recorded blocker/cancellation path; do not create a replacement claim or directly adjust a lease. |
| Git/model unavailable or credential rejected | Check configured destination, TLS, credential-file presence and network snapshot. Failed requests may retain model reservations when upstream usage is uncertain. | Restore the endpoint or rotate credentials using the sequence below. Refresh paired DNS/IP rules using NETWORK.md if addresses retired. Observe bounded retry or the existing blocker. Create a new authorized follow-up for terminal work. Never switch endpoints implicitly, clear reservations, or repeatedly rerun uncertain external mutations. |
| Cancellation does not acknowledge | Inspect the durable Deck command and its run, Marina readiness, the matching Job/Pod and API/RBAC access. Lack of acknowledgement must not be reported as cancellation success. | Restore controller/API access so Marina can revoke authority, observe Job termination and clean resources. If the node is unreachable, restore it or independently establish that its processes are stopped before forced cleanup. Do not remove finalizers or force-delete a Pod merely to fabricate termination. Keep the command pending until the adapter can observe the outcome. |
| Worker fails / evicts / times out | Inspect only the attempt's labelled Job/Pod status, exit reason and runtime record. No successful current verification means no completed task. | Repair the resource, image, node, deadline or configuration problem. Let Marina collect available output and finish cleanup. The first release has no automatic repair loop: use a linked, newly authorized follow-up task, preserving the original blocker and artifacts. |
| A required check is failed, missing, or stale | Match local-check evidence, CI context and PR head in Deck/Git. Missing CI is a wait until deadline, not success. | Repair provider/check configuration or use a bounded follow-up task. A changed head requires new verification. Do not relabel old evidence as current or bypass branch/CI controls. |

These procedures follow the observed local credential/network failures and the
tested controller failure paths. A live-model outage, disk-full recovery and
active-worker cancellation have not yet been accepted on this installation.

## Renew certificates (OPS-03)

Keep the existing CA and identity files. Renew before either leaf expires:

```sh
python3 scripts/renew-installation-certificates.py ~/.local/share/the-platform/marina-local/installation
python3 scripts/verify-local-podman-operations.py --install-renewed-certificates ~/.local/share/the-platform/marina-local/installation
security add-trusted-cert -r trustAsRoot -p ssl -s localhost -k ~/Library/Keychains/login.keychain-db ~/.local/share/the-platform/marina-local/installation/deck.crt
```

The first command stages and verifies both certificates, preserves their DNS/IP
names, requires sufficient remaining CA lifetime, and leaves private rollback
copies in `certificate-backup-*`. It does not alter CA, tokens or identities.
The rehearsal requires disabled intake and no active work, installs only the
TLS Secrets, restarts both services, and proves the served Deck leaf, existing
session and task/agent IDs. It also performs the credential rehearsal below.
Clients using their own CA store continue to use `ca.crt`; the final macOS command
updates only the localhost leaf trust used by the browser.

If rollout fails, the rehearsal restores the previous cluster TLS Secret data
and restarts both services. Restore the matching four leaf files from the named
private backup before applying the whole overlay again. Check readiness before
ending maintenance. CA replacement needs an overlapping trust bundle distributed
to both services and workers before new leaves are installed; this helper does
not rotate or extend the CA.

## Rotate credentials (OPS-04)

Operator and service policies reload on requests; credentials must not appear in
arguments, task text or evidence. Keep stable operator and service identity IDs.

1. Add the new token fingerprint alongside the old fingerprint in the relevant
   private auth policy. Update `deck-auth` and wait until a new-token request works.
2. For the service token, replace the private `deck-token` and mounted
   `marina-credentials` Secret. Wait for projection and confirm `/marina doctor`
   works. The file is read on each request; no identity recreation is required.
3. Remove the old fingerprint. Confirm old-token requests fail, new-token
   requests work, and task/run/agent IDs remain unchanged.
4. Operator token revocation prevents new logins. Existing sessions intentionally
   survive until logout or their 12-hour expiry; token rotation does not revoke
   them. Log out each rehearsal session explicitly.

For a repeatable idle local rehearsal, run:

```sh
python3 scripts/verify-local-podman-operations.py ~/.local/share/the-platform/marina-local/installation
```

It temporarily rotates operator/service credentials and then restores the original
Secret data and revokes its temporary tokens. It leaves private operator/token
files unchanged. If interrupted, restore `deck-auth` and `marina-credentials`
from the private installation and verify readiness before admitting work.

For Git/OpenRouter, issue a replacement credential with the same repository/model
scope, replace `git-token` or `model-token` privately, update the mounted Secret,
verify an authorized bounded request, then revoke the old credential at its issuer.
Both gateways and the Git API client read these files per request. Empty or missing
configured files fail locally; missing model credentials do not reserve budget.
Upstream rejection or a lost response may retain a reservation because usage is
uncertain. Provider revocation must be checked at the real provider before OPS-04
is complete. An unauthenticated model endpoint must be an explicit configuration
choice (`token_file: ""`), never an empty configured credential file.

The attempt signing key is loaded at service startup. Rotate it only after draining
workers and restart Marina; existing attempt tokens otherwise become invalid.
Artifact download credentials reload on requests, but both Deck and Marina must
receive the replacement together; verify a digest-checked download afterwards.

## Artifact retention (OPS-05)

The [retention decision](../../../docs/decisions/0013-conservative-artifact-retention.md)
preserves all referenced historical evidence. Upgrade to the new Marina binary,
finish/cancel active work, stop Marina, and run maintenance with its complete
journal and artifact volume. Deck can remain available, but artifact downloads
are unavailable during this maintenance window. Disabling intake is insufficient.

For directly mounted volume paths:

```sh
umask 077
marina artifact-retention --journal-path /data/marina.db --artifact-directory /data/artifacts --min-age 168h --limit 100 > /private/tmp/marina-retention-plan.json
# Review the candidate hashes, sizes, timestamps and total count.
marina artifact-retention --journal-path /data/marina.db --artifact-directory /data/artifacts --apply-plan /private/tmp/marina-retention-plan.json
```

On Kubernetes, scale only Marina to zero after draining work, wait for its Pod to
terminate, and mount `marina-data` in a temporary non-root maintenance Pod running
the updated pinned Marina image. Run the same command with `/marina`; save its
dry-run JSON logs privately, then mount that reviewed JSON as a read-only ConfigMap
for the apply Pod. Use the same `/data` mount and UID 65532. The maintenance Pod
needs no service-account token or network access. Remove only those temporary Pods
and the plan ConfigMap, restore the original replica count, and verify readiness
and existing evidence downloads. Never mount another database or a logical export
in place of the real journal.

The command refuses an in-use store, active executions, an unrelated directory,
expired reports or changed candidates. Seven days and 100 files are defaults;
the supported lower age bound is 24 hours and the upper batch size is 1,000.
Corrupt candidates stop collection. Unknown filenames, symlinks and pending writes
are not deleted. Referenced artifacts remain indefinitely, including failed and
completed work. After partial deletion, retain the reported count and generate a
new dry run. Do not widen deletion to make a full disk fit.

## Finish maintenance

Confirm both services are ready, stable IDs and original task states remain,
cancelled Jobs stay stopped, and current evidence still downloads with matching
digests. Restore the previous intake setting only when live execution configuration
and authority are ready. Keep local evidence separate from laptop-off acceptance.
