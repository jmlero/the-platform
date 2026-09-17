# Decision 0011: Validate repository readiness and recover channel history

- Status: accepted implementation boundary
- Date: 2026-09-17
- Scope: AppDeck entry points and platformctl

Tasks with explicit `source.*` or `pull_request.*` permissions identify repository
work for the current PR workflow. Creating, moving or editing such work in Ready
requires observable acceptance criteria, at least two attempts, and explicit
`source.read`, `source.write`, and `pull_request.create` authority. Incomplete work
can remain in Backlog. AppDeck does not infer coding intent from task prose or
silently grant authority. Generic tasks retain provider-neutral compatibility.
Marina independently checks every claimed task, including legacy/generic work;
AppDeck admission is not a promise of installation readiness.

Channel promotion reuses the task form so budgets and PR permission cannot diverge.
Posting a message atomically stores a correlated event. Messages page in durable
append order using an exclusive, opaque cursor naming a message in the same
channel. This avoids timestamp ties and offset drift during concurrent appends.
The browser follows all pages on selection, message events and reconnection,
replacing history by message identity and discarding superseded requests.

The CLI exchanges an operator token for an in-memory session, sends CSRF protection
on mutations and attempts logout at command completion. It neither persists cookies
nor sends the operator token as a service bearer credential. Failed/expired sessions
do not trigger automatic mutation replay. Protected transport and redirect refusal
apply to the login exchange as well as authenticated operations.

These corrections do not add new execution modes: the supported pair remains
separate HTTP/JSON services, with cancellation and a one-pass repository workflow
ending at a PR ready for human review or a blocker. Future headless packaging,
pause/resume, revision loops, semantic review and merge/deploy authority must be
implemented and verified before being advertised.
