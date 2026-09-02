---
name: commit
argument-hint: "[-a|--all] [-c|--convo] [context]"
description: >
  Convention-aware commits that scope session changes, preserve unrelated dirty
  state, and split logical change sets.
---

# Commit

Create safe, convention-matching git commits. Preserve unrelated dirty state.
Split only logically independent changes.

## Flow

1. **Scope before git state**
   - `-a` / `--all`: all pending changes.
   - `-c` / `--convo`: conversation-relevant changes only.
   - Free text can imply scope: "everything", "only docs", "fetch change".
   - If unresolved, inspect first. Ask only if unrelated changes exist.

2. **Assess**
   - Run `git status`, `git diff`, `git log -15`, etc.
   - Infer repo's message convention from **full** commit messages (subject +
     body), not just subjects. Use `git log -N` (never `--oneline`) so the body
     format, bullet style, and scope patterns are visible. Default: conventional
     commits with a body when the change is multi-faceted.
   - Classify changed files as in-scope, out-of-scope, or ambiguous. Changes you
     made or discussed are session-relevant. Compare other changes to the
     conversation goal and touched systems.
   - Stop if nothing needs committing.

3. **Plan and confirm**
   - If the scope unresolved ask user session-only vs all.
   - Propose grouped summary, commit grouping, and draft message(s).
   - Use one commit for cohesive work. Split only independent changes.
   - Ask before committing: proceed, adjust grouping, or edit messages.

4. **Execution strategy**
   - Whole cohesive scope: use plain git.

     ```bash
     git add <files>   # avoid `git add .` / `-A` unless scope is truly all
     git commit -m "message"
     ```

   - File or line subset while preserving other dirty state byte-for-byte: use
     the helper. Never hand-roll index manipulation.

     ```bash
     subcommit <path|path:ranges>... -m <msg> --yes [-n|--no-verify]
     ```

     A plain path selects the complete file. Ranges are `N` or `N-M`, separated
     by commas or supplied through repeated ranged targets. Use repeatable
     `--complete <path>` for a literal filename ending in valid range syntax.
     Complete files and changed regions may share one commit. A unique exact
     move automatically includes both endpoints. Name both paths for an edited
     move. The helper selects from the last commit to the current worktree,
     preserves untouched dirty paths, previews effective ranged patches, runs
     hooks in an isolated index, and refuses if any target misses or scope
     expands.

   - For split commits, process groups sequentially and verify each.

## Message Quality

- Match the detected repo convention.
- Subject under 72 chars, specific, action-oriented.
- Name the behavior/change, not the file.
- Add a body only when the subject cannot capture scope.
- Avoid vague subjects: `Update file.ts`, `fix things`, `chore: updates`.

## Safety

- Warn before staging sensitive files: `.env`, credentials, tokens, keys.
- Large diffs: summarize and confirm.
- Preserve pre-staged paths outside the selected scope exactly. On a changed
  selected target, current worktree selection supersedes prior staging. An
  unchanged target with staged-only changes refuses instead of discarding them.
- For scoped file or range commits, use `subcommit`.
- A missed target refuses the whole operation. Correct or remove it rather than
  accepting a partial commit.
- Helper refusal means possible scope expansion. Include or fix that scope, or
  use another approach only with explicit justification.
