# Marina disposable acceptance fixture

This repository deliberately contains one defect. `clamp` must constrain values
to the **inclusive** interval, including negative and single-value intervals.
Its upper-bound calculation is wrong. The initial CI failure is expected.

Run the trusted check with Node.js 22 or newer; no packages are installed:

```sh
node --test tests/clamp.test.cjs
```

Delegate: **Fix clamp's inclusive upper boundary without changing its public API
or tests. All seven regression tests must pass. Only src/clamp.cjs may change.**

Configure Marina with `allowed_paths: ["src"]`, the command above as the
`regression` check, and `required_ci: ["acceptance-regression"]`. Grant two
attempts and bounded source-read, source-write and pull-request-create authority.
The result is a PR for human review; do not merge it during acceptance.

GitHub Actions runs separately from the Kubernetes worker. Its checkout download
is CI setup traffic. The worker and its separate verification Job use the image's
Node.js standard library and need no runtime package downloads.
