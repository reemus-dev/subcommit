---
name: subcommit
description: >
  Make isolated commits of explicit files or changed line ranges while
  preserving unrelated index and worktree state. Use instead of hand-rolled
  patch staging, stash, reset, or temporary-index Git workflows.
---

Use normal Git for whole-repository commits and complete changes to tracked
files. Use `subcommit` for changed line ranges, untracked files without
pre-staging, mixed complete and ranged targets, or isolated automated commits.

Run `subcommit --help` for syntax, guarantees, and examples.
