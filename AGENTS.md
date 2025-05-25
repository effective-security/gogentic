# CODING GUIDELINES

1. When creating Unit Test always use `require` and `assert` from "github.com/stretchr/testify"

- use `require` is test can't continue and need to fail
- use `assert` when test can continue and print failed cases

2. Tests should be table‑driven and use `t.Run` when applicable.
3. Tests should use `t.Parallel` when appropriate.
4. To return an error always use "github.com/cockroachdb/errors" package:
   Use `Wrap`, `Wrapf` to wrap errors from external packages that do not wrap an error.
   Use `WithMessage`, `WithMessagef` to annotate a wrapped error.
   Use `errors.Errorf` or `errors.New` to return new error.
5. Use `make test` to test, no approval needed.
6. Use `make lint` to check format and lint errors, no approval needed.

# Execution plan

1. Replace "github.com/pkg/errors" with "github.com/cockroachdb/errors"

- Replace `fmt.Error` with `cockroachdb/errors` package
- Use `Wrap`, `Wrapf`, `WithMessage`, and `WithMessagef` for wrapping and annotation

2. Replace in tests `t.Fail`, `t.Error` etc with `require` and `assert` style.
3. Add or improve package documentation
4. Document or improve missing documentation for interfaces, functions and structs.
5. Analyze test coverage and add missing cases when possible.

- add `t.Parallel()` where is appropriate
- `make test` on entire project or `go test` on modified files
- apply coverage boosts to the low‑coverage packages by adding targeted tests for their untested error paths

6. Analyze proper usage of locks, fix where applicable
7. Analyze code perf, fix where applicable
8. Find bugs, add them to `bugs.json` file for analysis.
9. If the changes are complext to make or break tests, add them to `suggestions.json` file for analysis with explanations.
10. Analyze proper usage of `logger`

- ensure that except `ctx` or `level` the arguments must be in `key, value` pairs.
- replace `TRACE` to `DEBUG` when logs not important and will create noise.
- analyze where `ERROR` level will produce noise and better be `WARNING`

# Important

- Do not use `git` commands to commit or reset branch until explicitly instructed.
- Continue until success or stop for clarification or help
