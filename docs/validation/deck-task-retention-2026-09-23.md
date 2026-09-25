# Deck task deletion and archive

- Date: 2026-09-23
- Environment: local Podman/Kind, `kind-marina-local`, `marina-system`,
  `podman-machine-default-root`, HTTPS at `https://localhost:8443`.
- Deployed image: `localhost/the-platform/deck@sha256:1bc0f7a498be32f4b024a233385c0176549437017d1410bfa11caae69d5014ea`.

## Behavior

Unexecuted Backlog drafts can be permanently deleted after an inline
confirmation. Channel-promoted and workload-invoked tasks retain their replay
identity and must be archived instead. Tasks with runs cannot be deleted.

Inactive tasks can be archived and restored from the new Archived view. Archive
preserves status, runs, PR links, evidence and artifact metadata; it is not a run
transition. Ready tasks must first return to Backlog and active work must stop.
Archived tasks cannot be edited, requeued or claimed until restored. Both
mutations require the current task version and use the existing operator/CSRF
boundary. Migration 012 adds the optional archive flag without changing status
values or existing agent capabilities. Export/import preserves it.

## Verification

- New store and HTTP tests failed before implementation and passed afterward.
- Store tests cover stale versions, active/Ready protection, deletion after run
  history, conversation promotion replay, archive/restore, restart and portable
  export/import. Completed run, PR summary, evidence and artifact preservation
  are checked; portable exports retain metadata rather than artifact bytes.
- `make check` and `make conformance` passed, including real Deck processes with
  CLI and Marina consumers. Frontend checks reported zero errors or warnings;
  all 23 frontend tests passed.
- `npm --prefix deck/web run build` and `git diff --check` passed.
- Chrome showed Delete draft and its confirmation/Keep task controls. Archiving
  disposable draft `tsk_7da9c4afcb540ddd5a31` removed it from Backlog, exposed it
  in Archived with history and Restore task, and restoration returned it to the
  workspace. Archived task details offered neither editing nor requeue.
- Live HTTPS API checks passed archive, stale restore rejection (409), restore,
  deletion, subsequent 404 and absence from bootstrap. The disposable draft was
  removed; no existing operator task was deleted or archived.
- Final deployment inspection showed the pinned image and one available replica.

The image was built from the existing working tree, preserving ongoing KINT and
local UI work. A concurrent local test rolled Deck back during the first rollout;
the verified image and documented local no-sign-in setting were reapplied. The
imported image required an explicit containerd digest alias before Kubernetes
could start it without a registry pull. The private installation image pin was
updated. This task did not change Marina's deployment, intake or repository
configuration, and did not run a model or authorize repository work.
