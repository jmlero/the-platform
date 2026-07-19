# Durable Agency, Disposable Compute

Marina treats agent compute as cattle, not pets.

An agent process should not depend on a particular laptop, terminal, Pod, workspace, or conversation staying alive. The factory must be able to terminate any runtime attempt and replace it without losing task authority, execution history, checkpoints, artifacts, or truthful progress.

The important distinction is:

> Agent compute is disposable; agent identity, authority, and progress are durable.

## Why laptop-hosted agents behave like pets

A coding-agent session running on a developer laptop commonly depends on:

- one particular machine and working directory;
- personal credentials and local configuration;
- conversation history stored only on that machine;
- a human starting, supervising, and rescuing the process;
- uncommitted state that can disappear when the process or laptop fails.

Moving that same stateful session into a long-running Pod does not solve the problem. A manually maintained Pod with personal credentials and irreplaceable local state is still a pet.

## Marina's execution model

| Concept | Treatment |
| --- | --- |
| Harness definition | Versioned code and configuration |
| Harness image | Immutable image digest |
| Logical agent identity | Durable identity and declared capabilities |
| Task and execution | Durable, versioned records |
| Runtime attempt or Pod | Disposable compute |
| Workspace | Recreated from a pinned source revision |
| Context | Immutable, provenance-rich package |
| Credentials | Scoped, short-lived, and authority-bound |
| Checkpoints and artifacts | Stored outside the runtime attempt |
| Failure recovery | Replace compute and continue from durable state |

The harness is a reusable execution definition: for example Codex, Claude Code, or a custom agent combined with its tools, sandbox, context handling, permissions, and output protocol.

A runtime attempt is one bounded invocation of that harness for one execution. The first Kubernetes adapter should map each attempt to an ephemeral Job. If an attempt fails, Marina inspects the durable record, fences the failed attempt, and starts a replacement only when policy, authority, and remaining budget permit it.

A logical agent is the durable actor behind those attempts. It has a stable identity, capabilities, authority, budgets, and audit history, but it does not require a permanent process.

## Core invariant

> No correctness-critical state may exist only inside an agent process, workspace, model conversation, container, or Kubernetes Job.

Consequently:

- task intent, plan revisions, authority, leases, budgets, and lifecycle state are persisted before execution;
- checkpoints describe durable progress rather than relying on conversational memory;
- diffs, commits, logs, evidence, and other outputs are written to durable external systems;
- repository workspaces are reconstructed from pinned revisions and verified inputs;
- runtime attempts receive only the credentials and network access required for their bounded task;
- cancellation, lease expiry, or budget exhaustion revokes authority even if old compute is still running;
- operators recover failed work by reconciliation and replacement, not by entering and repairing a specific container.

## Portable harnesses

"Run anywhere" means portable lifecycle semantics, not pretending that all infrastructure is identical.

The same harness specification may execute through:

- a Kubernetes Job;
- a local process or container for development;
- a managed agent runtime;
- a hosted coding-agent service;
- another scheduler added later.

Every runtime adapter must preserve the same observable contract for starting, inspecting, resuming when supported, cancelling, collecting artifacts, enforcing deadlines, and cleaning up attempts. Kubernetes-specific concepts remain inside the Kubernetes adapter rather than becoming Marina's public product model.

## Operational consequences

Disposable compute requires more than scheduling a container. Marina must provide:

1. Immutable runtime specifications and versioned harness profiles.
2. Idempotent start and cleanup operations.
3. Stable execution and attempt identifiers for rediscovery.
4. External checkpoints, artifacts, and evidence with content digests.
5. Short-lived workload identity and fenced authority.
6. Explicit CPU, memory, storage, process, network, time, attempt, token, and cost limits.
7. Heartbeats and normalized events that do not become the only record of progress.
8. Recovery that starts from durable observations rather than assumptions about a lost process.

This model applies equally to Codex and to future harnesses. Codex can be the first disposable coding worker without becoming the factory itself: Codex performs the bounded engineering task, Marina owns durable control and evidence, and the runtime provider supplies replaceable compute.

## Principle

Marina's execution philosophy is:

> **Durable agency, disposable compute, reproducible harnesses, externalized state.**

That is what makes an agent genuinely restartable, portable, and independent of an operator's laptop.
