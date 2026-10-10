# Decision 0031: Explicit Marina profiles for each project, one active task

- Date: 2026-10-04
- Scope: PROJ-01 source implementation
- Builds on [0014](0014-project-repository-and-knowledge.md)
- Acceptance and milestone prerequisite are in [status](../status.md) and [backlog](../backlog.md)

Marina accepts the existing single workflow configuration or a version-1
installation document containing up to sixteen complete project profiles. Each
profile names one Deck project, its repository, checks, allowed paths, required
CI, models, credential file references, verifier and native engine selection.
The profiles are operator-managed private configuration. Deck repository requests
cannot create profiles, credentials or additional source authority.

All profiles share one Marina process, journal, installation/controller identity,
service TLS and artifact store. Repository snapshots, profile digests and budgets
remain versioned per project. Requests route by the recorded attempt or turn's
project; the selected gateway still validates the original credential and live
fence, scope, inputs and budget. Missing projects fail closed. Each gateway also
rejects another project's attempt. Artifact access retains its existing separate
service credential and Deck authorization boundary.

One control cycle considers ready work from explicitly configured projects. Any
active execution or unresolved acquisition prevents another claim anywhere in
the installation, including after a lost response. Commands dispatch to the
recorded project's workflow; lease recovery and outbox publication retain the
shared durable controller. Adding projects does not enable parallel task work.

A profile can use project-specific Git/model credential references and packaged
toolchains, while service signing/TLS/artifact settings must agree. Duplicate
project profiles or inconsistent shared settings fail offline validation. The
installation preflight checks workload and conversation grants for every project;
startup requires the mounted credential files. The original single-profile
configuration and its digest keep their semantics.
