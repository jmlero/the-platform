<!--as:block/completion-evidence-->
## Completion evidence

Before claiming work complete, run the smallest relevant checks, then expand to
the repository's required suite when risk warrants it. Inspect resulting
behavior or diffs where useful. Report the checks run, failures, and anything
skipped. Never claim a check passed unless it actually ran.
<!--/as:block/completion-evidence-->

<!--as:block/focused-changes-->
## Focused changes

Keep edits tied to the requested outcome, including necessary supporting
refactors. Preserve unrelated user work and follow the surrounding code style.
Leave unrelated code, comments, and formatting alone. Remove imports, variables,
and functions made unused by your changes; leave pre-existing cleanup for a
separate task unless it is requested or needed for this change.
<!--/as:block/focused-changes-->

<!--as:block/no-unfinished-ui-->
## No unfinished UI

Do not expose controls, links, routes, or placeholders for an unavailable
feature. Either implement a coherent, usable path or omit it. Show a disabled
or placeholder state only when the product explicitly requires communicating
planned or temporarily unavailable behavior.
<!--/as:block/no-unfinished-ui-->

<!--as:block/ponytail-->
## Minimal implementation

- Choose the smallest safe change that fully meets current requirements.
- Reuse project code, standard libraries, native features, and installed
  dependencies before adding code or dependencies.
- Avoid speculative abstractions, configuration, extension points, and wrappers.
- Fix shared root causes; preserve validation, error handling, security,
  accessibility, and data safety.
- Verify non-trivial behavior with the smallest relevant checks.

Adapted from DietrichGebert/ponytail (MIT License).
<!--/as:block/ponytail-->

<!--as:block/secure-defaults-->
## Secure defaults

At new external boundaries, validate untrusted input and constrain sizes,
ranges, paths, and results. Require authentication and authorization for
sensitive mutations. Return safe client errors while retaining diagnostic
detail server-side. Use least privilege by default. Relax a protection only
when explicitly requested, and record the resulting risk in the repository's
normal task system, or in your handoff when there is none.
<!--/as:block/secure-defaults-->

<!--as:block/tdd-->
## Test-driven development

For features and bug fixes, repeat this cycle until the requirements are met:

1. Write a failing test that describes one expected behavior.
2. Make the smallest change that passes it.
3. Refactor the code you touched while keeping the tests green.

Keep tests independent, name them by behavior, mock external boundaries when
appropriate, and run the relevant checks after each change.
<!--/as:block/tdd-->

<!--as:block/transparent-shortcuts-->
## Transparent shortcuts

Never hide a shortcut or deferred production concern. When necessary work is
deliberately postponed, record it in the repository's normal task system, or in
your handoff when there is none. State the concrete risk and reason, and tell
the user. Do not create debt for work that is merely out of scope or
speculative.
<!--/as:block/transparent-shortcuts-->
