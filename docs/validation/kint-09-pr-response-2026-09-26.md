# KINT-09 lost successful GitHub PR response

- Date: 2026-09-26
- Scope: authorized one-time review PR in disposable
  [`jmlero/marina-acceptance` PR #14](https://github.com/jmlero/marina-acceptance/pull/14).
- Result: a real GitHub PR was created once, its successful HTTP response was
  discarded before the source-control client saw it, and two later calls
  recovered the same PR by its operation marker. The PR is open and unmerged.

The prepared branch `marina/kint09-pr-response-20260926` was based on
`10a96c6221f5c8a128ccb2617d8be27213d7b296`. Its commit
`e6c72271da1fee8cdbdb85d54969a4fd905460d8` used the required
`jmlero@hotmail.com` author and committer email. Only `src/clamp.cjs`
changed: the existing inclusive clamp now preserves the upper bound, and an
exported `clampExclusive` helper constrains integers strictly inside valid
bounds. The existing seven Node tests failed in four cases before the change
and passed all seven afterward. Focused strict-bound assertions and
`git diff --check` also passed.

An opt-in [GitHub client test](../../marina/internal/sourcecontrol/github/live_response_test.go)
used the private local Git credential reference, an exact branch/head and an
authority fence. Its transport forwarded the `POST /pulls` request to the
real GitHub API, received HTTP 201, then discarded that response. The first
`CreatePullRequest` returned the injected lost-response error. Two subsequent
calls with the same idempotency key recovered the original PR. A separate
GitHub list showed only PR #14 for that branch; a read-only PR inspection
confirmed head `e6c72271da1fee8cdbdb85d54969a4fd905460d8`, only
`src/clamp.cjs` in the diff, and no merge commit. GitHub's
`acceptance-regression` check passed on the PR head.

This validates the real GitHub adapter's PR-response recovery path. The
branch and commit were prepared locally; this test did **not** inject the
fault through a full Deck → Marina → kagent repository execution or its
outbox. Lost branch/commit response and full-process publication recovery
remain open in the [KINT-09 core matrix](../plans/kagent-integration.md#mandatory-failure-matrix).
KINT-11 remains gated. No merge or deployment was performed.
