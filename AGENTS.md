# CODING GUIDELINES

1. When creating Unit Test always use `require` and `assert` from "github.com/stretchr/testify"

- use `require` is test can't continue and need to fail
- use `assert` when test can continue and print failed cases

2. Tests should be table‑driven and use `t.Run` when applicable.
3. Tests should use `t.Parallel` when appropriate.
4. Avoid using `AnyTimes()` in Mock calls, always try to set an expected Times()
5. To return an error always use "github.com/cockroachdb/errors" package:
   Use `Wrap`, `Wrapf` to wrap errors from external packages that do not wrap an error.
   Use `WithMessage`, `WithMessagef` to annotate a wrapped error.
   Use `errors.Errorf` or `errors.New` to return new error.
6. Use `make test` to test, no approval needed.
7. Use `make lint` to check format and lint errors, no approval needed.
8. Use `make covtest` to see coverage

# Execution plan

- analyze code coverage
- select a package with least coverage
- create Unit Tests for that package
- ensure the coverage at least 86%
- test and lint
- stop and wait for review

# Important

- Do not use `git` commands to commit or reset branch until explicitly instructed.
- Do not create any folders or modify permissions on files.
- Continue until success or stop for clarification or help.
