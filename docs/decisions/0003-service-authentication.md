# Decision 0003: Use scoped opaque credentials for initial service authentication

- Status: accepted
- Date: 2026-07-19
- Scope: Marina-to-AppDeck service authentication and authorization

## Context

Marina must call AppDeck as a stable installation identity before automatic work intake can be enabled. AppDeck must distinguish that service from the human operator, limit it to configured projects and protocol capabilities, reject missing or stale credentials, and support rotation without changing logical agent or run identity. Secret values must never enter either product database, logs, events, tasks, plans, artifacts, or source control.

The first installation is single-operator and Kubernetes-first, but the work-control contract must not depend on Kubernetes identity APIs. Human sessions are a separate concern: an authenticated service credential must not become an operator credential.

## Options considered

### Mutual TLS

mTLS provides strong transport and peer authentication, but certificate issuance, rotation, trust bundles, and local development add operational machinery before the first control-plane proof. Project and capability authorization would still require an AppDeck policy after certificate validation.

### Kubernetes service-account tokens or TokenReview

Workload identity fits one cluster well, but direct TokenReview would make AppDeck's public service boundary Kubernetes-shaped. AppDeck and Marina must also work as local processes and remain portable to a different workload-identity provider.

### OIDC access tokens

OIDC is the preferred longer-term federated mechanism, but it requires choosing an issuer, audience, key-discovery lifecycle, and claim model that the first installation has not selected. Those choices should not be made accidentally through a library.

### Scoped opaque Bearer credentials

A high-entropy opaque token is simple to issue for one installation and independent of the runtime substrate. AppDeck can map a one-way fingerprint to stable identity and authorization policy while Marina resolves the secret from a file at request time.

## Decision

Use scoped opaque Bearer credentials for the initial Marina-to-AppDeck mechanism.

- Marina sends the credential in the HTTP `Authorization: Bearer` header. Tokens are never accepted in a URL, query string, request body, or log field.
- Each token contains at least 256 bits of cryptographically random material.
- Marina receives only a file reference through `MARINA_APPDECK_TOKEN_FILE`. It rereads that file for each request, keeps no token in the journal, and does not bind run identity to the credential value.
- AppDeck receives a versioned policy path through `APPDECK_SERVICE_AUTH_FILE`. The policy contains stable identity IDs, SHA-256 token fingerprints, explicit project IDs, and explicit work-control capability IDs. Wildcard project and capability grants are rejected.
- AppDeck rereads and validates the policy for each authenticated request. Multiple fingerprints may temporarily map to the same identity, permitting overlap during rotation. A malformed or unavailable replacement fails closed with a retryable service-unavailable response.
- Authentication failure returns `401 unauthenticated`; a valid identity outside its project or capability grants returns `403 forbidden`. Both use the versioned structured error envelope and request correlation ID.
- Project scoping is resolved from canonical AppDeck resources. Collection responses are filtered to allowed projects; mutations are rejected before state changes. Projectless internal events are not exposed on the service event stream.
- The authenticated service identity is distinct from Marina's logical agent and runtime-attempt identities. Later guarded run mutations must still verify the logical agent, resource version, and lease fence.
- Bearer credentials require a protected transport. Outside single-host loopback, use TLS or an authenticated encrypted service mesh. A network policy alone does not make plaintext credentials acceptable for an exposed installation.

AppDeck's operator routes use the distinct human-session mechanism recorded in [decision 0005](0005-operator-authentication.md) when it is configured. Marina service credentials never authenticate a human session.

## Rotation procedure

1. Generate a new random token and its SHA-256 fingerprint.
2. Add the new fingerprint to the existing AppDeck identity while retaining the old fingerprint.
3. Atomically replace Marina's referenced token file.
4. Verify authenticated provider readiness with the stable identity unchanged.
5. Remove the old fingerprint from AppDeck's policy.

An authorization failure during a non-atomic update is safe: Marina performs no authorized mutation and retries only according to the operation's bounded failure policy.

## Consequences

- The first service identity and authorization model are provider-neutral and require no new dependency.
- A leaked token remains a Bearer credential until revoked, so transport protection, narrow scopes, file permissions, rotation, and redacted diagnostics are mandatory.
- AppDeck policy distribution and Marina secret distribution remain separate deployment responsibilities. The repository contains neither token values nor usable fingerprints.
- Human authentication, run idempotency, resource versions, and fence tokens are separate safety layers; this decision does not replace them.

## Replacement criteria

Replace token validation with OIDC or workload identity when the always-on environment has a stable issuer and audience, when several installations make manual rotation error-prone, or when short-lived automatically issued credentials materially reduce risk. Preserve the same stable-principal, explicit-project, explicit-capability authorization semantics and the work-control error contract.
