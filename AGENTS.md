# subcommit

`subcommit` is a Go CLI for committing explicit worktree changes while
preserving unrelated repository state. Git 2.36+ is its only runtime dependency.

Read `README.md` for the user contract.

## Where to work

| Area                                     | Responsibility                                   |
| ---------------------------------------- | ------------------------------------------------ |
| `subcommit.go`                           | Entry, signals, and both executable names        |
| `internal/cli/`                          | Cobra commands, parsing, and completion bridges  |
| `internal/console/`                      | User-facing output and terminal behavior         |
| `internal/subcommit/`                    | Transactional selection and publication engine   |
| `internal/gitexec/`                      | Shell-free Git process boundary                  |
| `internal/patch/`                        | Unified-diff parsing and range filtering         |
| `internal/completions/`                  | Release completion generation                    |
| `.goreleaser.yaml`, `.github/workflows/` | Release packaging, native CI, and publishing     |
| `acceptance/`                            | Black-box behavior, hooks, races, and Git parity |
| `skills/`                                | Minimal CLI and opinionated commit agent skills  |

Within `internal/subcommit`, `subcommit.go` coordinates the operation.
`complete.go`, `ranges.go`, and `moves.go` select changes. `commit.go`,
`hooks.go`, and `finalize.go` own commit creation and publication. `entry.go`,
`status.go`, and `path.go` model Git and worktree state.

There is no public Go library contract. Keep engine APIs internal.

## Design constraints

- Never intentionally modify, restore, or roll back the worktree.
- Derive selection from `HEAD` to the complete worktree. The real index is not a
  selection input. After publication, selected index entries align with the new
  `HEAD` while every unrelated index entry remains exact.
- Build candidates through Git plumbing and an isolated temporary index. Publish
  under `.git/index.lock`, guard the `HEAD` update, and retain recovery material
  when `HEAD` moves but index publication fails.
- Treat targets as literal paths. Runtime Git arguments never pass through a
  shell, and engine-only Git environment settings must not leak into hooks.
- Match native Git path and mode semantics rather than adding surprising
  guardrails. Preserve the documented behavior for symlinks, `skip-worktree`,
  `core.fileMode`, and `commit.gpgsign`.
- Hooks may not change selection scope. Complete-file targets may be formatted
  and restaged while remaining changed. Range-selected targets may not mutate.
  Inferred moves must retain both endpoints.
- Concurrency excludes cooperative index writers and guards `HEAD`. The worktree
  remains externally mutable.

## Development

Install the locked toolchain with `mise install`. `.mise.toml` is the authority
for commands and tool versions. Run the complete gate with:

```sh
mise run check
```

Keep acceptance tests black-box. `SUBCOMMIT_TEST_BIN` selects the implementation
under test. Use compiled helpers for hooks and races so fixtures remain native
on Windows.

CI and release workflows must install the tracked mise toolchain and invoke the
same mise tasks used locally. Do not introduce independent version sources.
