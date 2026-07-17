# Contributing

## Why a monorepo

Solo developer, everything ships together, no independent consumers. Separate repos
would buy nothing and cost cross-cutting changes across five PRs — and the change
you'll make most often (edit a workflow JSON *and* the Go builder that patches it)
is exactly the one that spans boundaries.

Each toolchain roots in its own subdirectory (`core/go.mod`,
`orbit/package.json`, `eval/requirements.txt`). The root holds only orchestration.
A root-level `go.mod` would try to own `orbit/` and `eval/`.

`workflows/` is top-level rather than inside `core/` because it's data consumed by
both the Go builder and the ComfyUI image — and it's the thing edited most.

## When to split a repo out

A repo boundary should exist because **deployment or ownership differs**, not because
code is conceptually distinct. Right now neither differs.

Candidates, in likely order:

| Split | When |
|---|---|
| `comfy/` + `workflows/` | The GPU image deploys on a different cadence than the app. Likely, eventually. |
| `compliance/` | It needs its own scaling or a separate audit boundary. Watch this one — phase 4 may force it. |
| `training/` | If it happens at all — genuinely different lifecycle. |
| `manga/` | Phase 7, if it grows past a package. |

## Conventions

**Go**
- Standard layout. `internal/` for everything not imported externally.
- `assembly`, `nltag`, and `workflow` are pure — no I/O, no excuse for thin coverage.
- **Don't add dependencies to `assembly`.** If it needs I/O, the design is wrong;
  hoist it to the caller.

**Workflow JSON**
- API format only (not editor format).
- Version by content hash. Never edit in place without bumping.
- Node IDs live in `internal/workflow/nodes.go` and break on re-export. Update them
  in the same change.
- Pin any new custom node in `comfy/` in the same change.

**Migrations**
- Forward-only, numbered.

**Tests**
- Everything before the GPU is testable without one. Steps 1–5 of the request flow
  are pure. There's no excuse for a test suite that needs a 4090.

## The things that will bite you

Documented at length elsewhere; listed here so they're findable:

1. API-format vs editor-format JSON → `.claude/skills/comfy-workflow/`
2. Node IDs renumber on re-export → same
3. Settings don't transfer between model lineages → `docs/tuning.md`
4. `min_post_count`, not CFG, is the accuracy lever → `docs/tagdb.md`
5. IP-Adapter FaceID doesn't work on anime → `docs/precise-reference.md`

## Before opening a PR

- [ ] Pure packages stayed pure
- [ ] `blocked` handled if the change touches job flow
- [ ] Both compliance hooks wired if the change adds a generation path
- [ ] Node IDs and content hash updated if a workflow changed
- [ ] Custom nodes pinned if a workflow gained a dependency
- [ ] Tested against both lineages if sampling changed

## Non-negotiable

`docs/compliance.md` is not a policy document to route around. Changes that touch
training data or output delivery preserve the filtering, or they don't merge. This
isn't bureaucracy — it's the line between a lawful product and a criminal liability,
and the reasoning is in the doc.

## Commits

Conventional commits, checked by [commitizen](https://commitizen-tools.github.io/commitizen/)
via a versioned `commit-msg` hook:

```bash
brew install commitizen   # once per machine
make hooks                # once per clone
```

Format: `type(scope): subject` — types `feat`, `fix`, `refactor`, `docs`,
`test`, `build`, `chore`, `perf`, `ci`; scopes are the top-level areas
(`core`, `orbit`, `comfy`, `workflows`, `db`, `nltag`, `tags`, …).

```
feat(orbit): wire pose tabs to per-role images
fix(core): retry live tag lookup through active aliases
```

`cz commit` walks you through it interactively; a plain `git commit` is fine
too — the hook rejects anything off-format. History before the hook predates
the convention.
