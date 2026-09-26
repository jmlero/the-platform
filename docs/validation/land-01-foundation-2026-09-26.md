# LAND-01: guarded main-update foundation

- Date: 2026-09-26
- Environment: Marina source tests with an isolated HTTP GitHub double.
- Result: the source-control port can fast-forward a configured base branch to
  an exact task-branch commit and reconcile an applied update whose response
  was lost. The repository workflow still opens a pull request; LAND-01 remains
  Open.

The new optional landing port requires the configured base branch, a
`marina/` task branch, the expected base and verified task commit, and a live
authority fence. It checks both refs before a non-forced GitHub update. If the
update response is lost, it reads the base ref: an exact target match is the
same successful operation and a retry does not send another update. A changed
base or task branch blocks before mutation. The test exercised a real HTTP
connection drop after the fake provider applied the update; two client calls
made only one PATCH. `go test ./internal/sourcecontrol/github -count=1` passed.

This is a primitive, not a landable workflow. LAND-01 still needs a distinct
versioned workload profile and authority, the controller sequence from clean
verification through branch CI to main, synchronized Deck/Marina contract and
fixtures, an acceptance repository with suitable branch protection, product
checks, conformance, and one real local Kind task. Existing `repository/v1`
tasks must keep their pull-request semantics.
