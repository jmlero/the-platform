# Decision 0024: Marina provisions each engine attempt's template

- Date: 2026-09-27
- Status: accepted and implemented
- Scope: how chat turns and repository attempts obtain their kagent
  AgentTemplate, ModelConfig and gateway Secret; the grant Marina holds in the
  engine namespace. Refines [decision 0017](0017-kagent-core-engine.md) and the
  KINT-04 gateway contract.

## Context

The pinned egress router caches the injected gateway credential, so rotating a
Secret does not revoke it (KINT-04). Each attempt therefore needs its own
Secret, and because a ModelConfig names its Secret and a template names its
ModelConfig, its own template. Until now an operator prepared one such slot per
chat turn with a helper script, edited the private workflow, and restarted
Marina. Every rotation changed the installation profile digest, which blocked
active repository work and forced repository re-association. The chat could not
be used turn after turn, and repository work could not move to kagent.

## Decision

- Marina derives a `marina-` name from the durable attempt key and creates the
  attempt's Secret (with a placeholder), ModelConfig and AgentTemplate from
  values pinned in its code and the installed profile. It records the prepared
  revision in the attempt specification before writing the attempt credential,
  and deletes the three resources after the engine instance is gone.
- The installed profile names the engine, harness and namespace, never a
  template or revision. No template or Secret serves two attempts, and the
  journal still refuses a reused template name.
- Marina never adopts or deletes a resource without its
  `the-platform.dev/owner: marina` label, or one whose content differs from
  what the attempt defined. A restart or lost response finds the attempt's own
  resources by name.
- In the engine namespace Marina may get, create, patch and delete Secrets and
  get, create and delete ModelConfigs and AgentTemplates. This namespace holds
  the engine database Secret. The local installation accepts this grant; the
  name and label checks in Marina are the guard against touching other
  resources.

## Consequences

- Chat turns and repository attempts need no operator preparation or restart.
  The per-turn helper script is removed.
- Installed profile digests no longer change between turns, so repository work
  keeps its binding while the chat is used.
- A cluster or multi-user installation should narrow the Secret grant, for
  example with a dedicated namespace or an admission policy; that belongs with
  the hardening in KINT-17.
