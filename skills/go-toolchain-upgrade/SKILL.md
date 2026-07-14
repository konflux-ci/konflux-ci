---
name: go-toolchain-upgrade
description: >-
  Use when konflux-ci/konflux-ci PRs touch go.mod, go.sum, the go directive,
  golangci-lint or controller-gen pins, or deploy/e2e scripts that run go or make;
  or when CI logs show go.mod requires go >=, GOTOOLCHAIN/local mismatch, or
  OpenShift Prow failures on deploy-konflux-on-ocp.sh (cd operator; make install) or controller-gen.
---

# Go Toolchain Upgrade

**Triage first.** Most `go.mod`/`go.sum` PRs are dependency-only—do not block them for
cross-repo checklists. Escalate only when minimum Go or CI **images** must change.

## Triage

| Signal | Result | Action |
|--------|--------|--------|
| Only `require`/`replace`/indirect changes; **`go` unchanged** | Routine | Short note; **stop** |
| `go.sum` only | Routine | Same |
| **`go` minor increases** (e.g. 1.25→1.26) in any module | Significant | → **Significant** |
| golangci-lint, controller-gen, or Makefile tool pins | Significant | → **Significant** |
| `deploy-konflux-on-ocp.sh`, `test/e2e/run-e2e.sh`, workflow Go pins | Significant | → **Significant** |
| PR states new language/stdlib minimum | Significant | → **Significant** |
| Unclear large dep bump | Check | If `go` unchanged and CI green → **Routine** |

Compare `^go ` in `operator/go.mod`, `test/go-tests/go.mod`, `operator/docs/go.mod`.
Patch within same minor (1.26.0→1.26.1) = **Routine** unless other signals apply.

## Routine (inform only)

```markdown
**Go toolchain (routine):** Dependency-only update; minimum Go unchanged. No
openshift/release or infra-deployments image changes expected. In-repo CI suffices.
```

Do not request **Go toolchain impact**, parallel PRs, or local `make test`/`make lint`.

## Significant (author plans cross-repo)

**In-repo:** Trust green `operator-test`, `operator-lint`, `operator-verify-generated-files`.
Do not ask the author to run them locally. Prow-only paths (`deploy-konflux-on-ocp.sh`:
`cd operator` then `make install`) are not covered by GitHub Actions—see [reference.md](reference.md).

**Author must identify** (link PRs; external repos may have no agent skills):

- openshift/release — `build_root` / golang tags for konflux-ci jobs; rehearse OpenShift e2e
- infra-deployments / legacy — `e2e-test-runner` if `test/go-tests` minimum Go rose
- `.github/workflows` / `.tekton/` only if this PR changes Go or builder pins there

Paths, PR body template, grep: [reference.md](reference.md). On significant PRs,
request changes if **Go toolchain impact** is missing.

## Common mistakes

| Mistake | Fix |
|---------|-----|
| Block every `go.mod` PR for openshift/release | Triage; dependency-only changes = routine |
| `GOTOOLCHAIN=auto` on RHEL Prow builders | Bump `build_root` image; `local` wins |
| Match golang tag to cluster variant (`ocp420` → 4.21 golang) | Builder stream ≠ cluster version; see reference |
| Fix only konflux-ci when `test/go-tests` `go` rose | Rebuild `redhat-appstudio/ci:e2e-test-runner` |
| Ask author to run `make test` locally | CI already runs on PR |

## Rationalizations (do not accept)

| Excuse | Reality |
|--------|---------|
| "go.mod changed → need full checklist" | Only if `go` directive or significant signals |
| "GA is green → merge" (significant) | Prow may still fail on older golang image |
| "We'll fix openshift/release after merge" | Plan parallel PR + merge order before merge |
| "Mintmaker/Renovate = platform upgrade" | Default routine unless `go` line moved |

## Related

- `go.mod requires go >=` in logs: [debug-e2e-tests](../debug-e2e-tests/SKILL.md), then re-triage here as significant
- Cross-repo map: [reference.md](reference.md)
