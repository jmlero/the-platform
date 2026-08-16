# Decision 0005: Use file-configured operator tokens and server-side sessions

- Status: accepted
- Date: 2026-08-15
- Scope: initial AppDeck human authentication

## Context

The always-on AppDeck interface cannot be exposed while its human routes accept anonymous reads and mutations. The first installation has one operator, no selected identity provider, and no requirement for password recovery or multi-user role management. AppDeck must still keep human identity separate from Marina's service credential, avoid persisting usable credentials, survive process restarts, resist cross-site request forgery, and fail closed when configured authentication is unavailable.

## Options considered

### Local passwords

A password flow would require a memory-hard password hashing dependency, enrollment, reset, and recovery behavior. Those choices add a user database before AppDeck has a multi-user requirement and would be superseded if the always-on environment supplies OIDC.

### Reverse-proxy authentication only

Delegating all authentication to an ingress proxy is operationally attractive, but trusting identity headers safely requires choosing and enforcing one proxy topology. It would also leave direct AppDeck access unauthenticated unless the network boundary were perfect.

### OIDC authorization code flow

OIDC is the preferred path when an issuer is selected. Choosing issuer discovery, client registration, callback URLs, group mapping, and session semantics before selecting the first cluster would make an infrastructure decision inside the product by accident.

### High-entropy operator token exchanged for a session

A one-time browser login using an externally issued opaque token is small, independent of the deployment substrate, and can establish a conventional short-lived browser session without turning Marina's credential into a human credential.

## Decision

Use a file-configured, high-entropy operator token to establish the initial single-operator AppDeck session.

- `APPDECK_OPERATOR_AUTH_FILE` references a versioned policy containing stable operator IDs and SHA-256 fingerprints of tokens with at least 256 bits of cryptographically random material. Raw tokens never enter configuration committed to source control, AppDeck's database, logs, events, or URLs.
- AppDeck reloads and validates the policy for each login and readiness check. Multiple fingerprints may temporarily map to the same operator during rotation. A malformed or unavailable policy rejects new logins and degrades readiness.
- A successful login exchanges the token for independently generated session and CSRF secrets. AppDeck stores only their SHA-256 digests, the stable operator ID, and timestamps in SQLite. Sessions survive process restart and expire after 12 hours.
- The browser receives a host-only `HttpOnly` session cookie and a separate CSRF cookie. Both use `SameSite=Strict`; authenticated browser requests must be same-origin, and mutations must echo the CSRF value in `X-CSRF-Token`. Cookies are `Secure` by default, and disabling that flag is permitted only for trusted local HTTP development.
- Every human read and mutation route, including bootstrap data and the event stream, requires the operator session when authentication is configured. Health probes and Marina's separately authenticated service protocol are unchanged.
- Logout deletes the server-side session and expires both cookies. Existing sessions remain valid across access-token rotation until logout or their bounded expiry; rotation is therefore not presented as immediate session revocation.
- Leaving `APPDECK_OPERATOR_AUTH_FILE` unset preserves the trusted local prototype workflow. An ingress or other untrusted-network exposure must configure operator authentication and TLS; the Kubernetes base remains cluster-internal and intentionally creates neither an ingress nor a secret.

## Consequences

- AppDeck now has an authenticated human boundary without a password database or identity-provider dependency.
- Human sessions are distinct from service identities and retain stable operator attribution for durable commands.
- Operators must securely generate, distribute, and rotate one high-entropy login token. Token entry is less convenient than an OIDC redirect but is acceptable for the initial single-operator installation.
- A stolen login token can create sessions until its fingerprint is removed; a stolen session remains usable until logout or expiry. TLS, secret-file permissions, secure cookies, bounded expiry, and CSRF enforcement are mandatory controls.
- Multi-user authorization, centralized revocation, recovery, MFA, and organization policy remain deliberately out of scope.

## Replacement criteria

Replace the token exchange with OIDC when the deployment has a stable issuer and callback domain, when more than one human needs distinct authorization, or when centralized MFA and revocation materially reduce risk. Preserve stable operator identity, server-side session revocation, CSRF protection, explicit separation from service credentials, and authenticated audit attribution.
