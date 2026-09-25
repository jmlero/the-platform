# Local Deck access and header simplification

- Date: 2026-09-23
- Environment: local Podman/Kind, `kind-marina-local`, `marina-system`,
  `podman-machine-default-root`, HTTPS at `https://localhost:8443`.
- Decision: [local pilot update](../decisions/0005-operator-authentication.md#local-pilot-update-2026-09-23).
- Deployed Deck image: `localhost/the-platform/deck@sha256:1666fcdfd939c8a869321981b4d799a8f728693281d6ad2de628e68bf7d20858`.

The local profiles omit `DECK_OPERATOR_AUTH_FILE`, using the existing `local`
operator mode. The private installation pins the updated image. The browser
header removes import/export, owner, and sign-out controls and retains search,
live connection state, and New task. No Marina deployment or credentials were
changed by this task.

## Verification

- The new local-profile regression test failed before the configuration change
  and passed afterward for both local Podman profiles. Service authentication
  remains configured.
- `make -C deck test check` passed: Go tests and vet, 23 frontend tests, and
  Svelte checks with zero errors or warnings.
- `npm --prefix deck/web run build` passed and refreshed the embedded assets.
- `make check-installation` passed all 19 tests.
- `git diff --check` passed.
- The local Deck deployment completed its rollout successfully.
- Requests with no token or session cookies returned HTTP 200 for the operator
  session (`operator_id: local`) and bootstrap; the service provider endpoint
  returned HTTP 401 without service credentials.
- Chrome displayed the populated work overview after refresh. Its header showed
  Search work, Live, and New task, with no import/export, owner, or sign-out
  controls. No login was required.

This evidence covers the requested local access and header changes. The image
was built from the existing working tree, including its ongoing KINT changes;
this task did not edit those changes. No new workflow execution or broader
engine acceptance was performed.
