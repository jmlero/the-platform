# KINT-09 permanent-installation failure follow-up

- Date: 2026-09-26
- Environment: permanent pinned kagent/Substrate on local `kind-marina-local`
- Status: partial; repository-work core matrix remains open

The [KINT-16 installation test](kint-16-2026-09-26.md) exposed a stale
Substrate worker route after the Podman VM restart. A new Deck chat invocation
created one kagent instance, but Substrate assigned worker IP
`192.168.180.175` while Kubernetes reported `192.168.180.190` for the same Pod
UID. The message delivery became uncertain. Marina inspected the original
instance, did not blind-resend, used zero model tokens, then published one
truthful deadline blocker and cleaned up the instance. Replacing the dedicated
worker Pod refreshed its UID/IP; a later paired template/ModelConfig/Secret
produced a real chat reply. This is a safe blocker observation, not transparent
recovery or a repository-work restart pass.

For pinned native Codex lifecycle checks, the locked harness image
`ghcr.io/kagent-dev/kagent/codex-harness@sha256:91f69cef4d02e30c078f866167a103853c4446f315e2aa59e6c4db8696881c85`
was loaded into Kind. The local no-inference fixture prepared revision
`feef39b964931dcdfd8e2473129076b03e6a6c4374c69da8b3545f995cd7f9e0`.
Temporary preparation egress was removed before invocation. Against a real
loopback port-forward to the permanent controller:

- `TestLiveIsolatedLifecycle` passed. A lost accepted create response recovered
  the original instance `01a0dc3a-219e-79c3-bf6a-7b547d7f5a6d`; a journal
  and connection reopen did not resend; a lost delete response reconciled.
  Zero model tasks were dispatched.
- `TestLiveAcceptedMessageResponseLoss` passed. After a successful A2A send
  response was hidden, the adapter reopened SQLite and gRPC and found the
  original instance `01a0dc3a-21fd-70ca-aea9-e5efe115371c` and task
  `01a0dc3a-2205-773f-bfdd-e227bdda03a8`. The send count stayed one.

The first sandboxed test command could not reach the loopback port-forward;
the same command outside the network sandbox passed both live tests. These
fixtures had no working inference endpoint or repository authority. They do
not close the decision 0023 core gate for real repository work, active model
and tool cancellation, budget exhaustion, failed verification/CI, or lost
commit/PR responses. KINT-10 and KINT-11 remain gated.

The no-inference AgentTemplate, Harness, ModelConfig and dummy Secret were
deleted after the tests. Preparation egress was closed and the loopback
port-forward stopped. The permanent chat resources and disabled intake remained.
