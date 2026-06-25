<!-- Line count is capped; see MAX_AGENTS_MD_LINES in .github/workflows/validate-agents-md.yaml -->
# AGENTS.md

## Project Overview

Konflux CI platform operator and deployment. Built with **Kubebuilder v4**, **Operator SDK**, and **controller-runtime**.

Main components:
- `operator/` — Kubernetes operator managing all Konflux services
- `operator/config/` — Kustomize layout for CRDs, manager, RBAC, OLM manifests
- `operator/upstream-kustomizations/` — Pinned upstream component versions
- `operator/pkg/manifests/` — Embedded upstream manifest content
- `operator/docs/` — Project documentation (Hugo site, source content in `operator/docs/content/`)
- `dependencies/` — Extra cluster layers (cert-manager, dex, quay, registry, tekton, kyverno)
- `integrations/` — Auxiliary scripts (sigstore, quay image-controller)
- `test/go-tests/` — Platform conformance tests
- `scripts/` — Local development helpers

**Three separate Go modules** — there is no root `go.mod`:
- `operator/go.mod` — the operator itself
- `test/go-tests/go.mod` — conformance test suite (separate dependency graph with Konflux service APIs, Tekton, GitHub/GitLab clients)
- `operator/docs/go.mod` — Hugo/Docsy documentation site

Always `cd operator` before running `make`, `go test`, or linting commands.

## Setup Commands

```bash
# Local Kind deployment
cp scripts/deploy-local.env.template scripts/deploy-local.env
# Fill in GITHUB_APP_ID, GITHUB_PRIVATE_KEY, WEBHOOK_SECRET
./scripts/deploy-local.sh

# Run E2E tests (after deployment)
cp test/e2e/e2e.env.template test/e2e/e2e.env
# Edit test/e2e/e2e.env with your values
source test/e2e/e2e.env
./test/e2e/run-e2e.sh
```

## Operator Makefile Targets

All targets run from the `operator/` directory:

- **`make test`** — Unit/integration tests (envtest, excludes e2e)
- **`make test-e2e`** — Operator e2e tests
- **`make lint`** / **`make lint-fix`** — golangci-lint (version in `operator/.golangci-lint-version`; must stay a single semver line—shared with CI **operator-lint**)
- **`make fmt`** / **`make vet`** — Go formatting and vetting
- **`make manifests`** — Regenerate CRDs and RBAC from code markers
- **`make generate`** — Regenerate deepcopy and other generated code
- **`make build`** / **`make run`** — Build or run the operator locally
- **`make docker-build`** — Build the container image
- **`make install`** / **`make uninstall`** — Install/uninstall CRDs into cluster
- **`make deploy`** / **`make undeploy`** — Deploy/undeploy operator to cluster
- **`make generate-docs`** / **`make docs-serve`** — Generate API reference docs / serve Hugo site

After changing APIs or RBAC annotations, run `make manifests generate` from `operator/`. CI enforces this via the `operator-verify-generated-files` workflow — PRs will fail if generated files are stale.

## Code Style

- Shell: `set -euo pipefail`, quote variables. Scripts that run on the user's host (deployment scripts, CLI helpers, and scripts stored in ConfigMaps that users fetch and run locally) must be compatible with both Linux and macOS — avoid GNU-only flags, prefer POSIX-compatible constructs, and test with both GNU and BSD coreutils (e.g. `sed`, `date`, `readlink`)
- Go: Standard formatting, Ginkgo for tests, Gomega for assertions/matchers (all test types: unit, functional, e2e)
- Kustomizations: Pin exact SHAs, not branches
- Markdown: Update TOC with `npx markdown-toc -i` if structure changes

## Testing

**Two distinct test suites:**

1. **Platform conformance** (`test/go-tests/tests/conformance/`) — end-to-end tests against a deployed Konflux instance, run via `test/e2e/run-e2e.sh`. Uses Ginkgo/Gomega with a shared `Framework` in `test/go-tests/pkg/framework/`.
2. **Operator unit/integration** (`operator/`) — controller tests using controller-runtime **envtest** (no real cluster needed). **Prefer Gomega matchers** for assertions in unit and functional tests. Shared test utilities in `operator/internal/controller/testutil/`. Run via `make test` from `operator/`.

**Test cleanup pattern — `DeferCleanupParentAndChildren`:**

Controller tests that create a parent CR and verify reconciler-managed children (ClusterRoles, ClusterRoleBindings, sub-CRs, etc.) **must** use `testutil.DeferCleanupParentAndChildren` for cleanup:

```go
Expect(k8sClient.Create(ctx, parentCR)).To(Succeed())
testutil.DeferCleanupParentAndChildren(k8sClient, parentCR,
    &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "child-role"}},
    &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: "child-binding"}},
)
```

Why: envtest has no garbage collector, so child resources must be cleaned manually. Ginkgo's `DeferCleanup` executes in LIFO order — registering children after the parent causes children to be deleted first, triggering the reconciler to recreate them (flaky timeout). `DeferCleanupParentAndChildren` deletes parent first (stopping reconciles), then children. The helper also re-issues deletes during polling to handle in-flight reconcile races.

```bash
# Kube-linter (before PR)
mkdir -p .kube-linter
find . -name "kustomization.yaml" | while read f; do
    kustomize build "$(dirname "$f")" > ".kube-linter/$(dirname "$f" | tr / -).yaml"
done
kube-linter lint .kube-linter
```

## CI Checks

PRs trigger the following workflows:
- **`operator-test`** — `go mod tidy -diff` + `make test`, uploads coverage
- **`operator-lint`** — golangci-lint
- **`operator-verify-generated-files`** — ensures CRDs/RBAC/deepcopy are up to date
- **`operator-test-e2e`** — full operator e2e (fork PRs require `/allow`)
- **`kube-linter`** — lints rendered kustomize manifests
- **`check-toc`** — validates markdown TOC (excludes `operator/docs/`, `.cursor/*`, `skills/*`)
- **`differential-shellcheck`** — ShellCheck on changed shell scripts
- **`caddy-fmt`** — verifies `Caddyfile` formatting (runs only when `Caddyfile` changes)

## PR Guidelines

- **Go / `go.mod` PRs:** Apply **go-toolchain-upgrade** (`skills/go-toolchain-upgrade/SKILL.md`) and follow its triage table—do not summarize the workflow from memory.
- **`.tekton` task/pipeline edits:** `pipeline.yaml` tasks `deploy-konflux-its` and `konflux-e2e-tests-its` hardcode `taskRef.revision: main`. To verify changes, temporarily point both at the PR’s git ref, run operator E2E, then restore `main` before merge (see `.tekton/pipelines/operator-e2e/README.md`).
- **Same-repo branches preferred**: E2E tests run automatically
- **Fork PRs**: Require maintainer `/allow` comment to trigger tests
- Run kube-linter before submitting
- Update TOC if markdown structure changed
- Run `make manifests generate` if API or RBAC annotations changed

## Architecture Notes

- Upstream components pinned via `?ref=SHA` + matching `newTag: SHA`
- Pipeline bundles (image digests) managed separately by Renovate
- Operator reconciles `Konflux` CR to deploy/manage all services
- APIs defined in `operator/api/v1alpha1` — many `Konflux*` kinds (Konflux, BuildService, IntegrationService, ReleaseService, UI, RBAC, etc.)
- Per-service reconcilers in `operator/internal/controller/<subservice>/`

## Skills

Detailed guides live in `skills/` — each subdirectory contains a `SKILL.md` with instructions.

| Skill | Use when |
|-------|----------|
| [go-toolchain-upgrade](skills/go-toolchain-upgrade/SKILL.md) | `go.mod`/`go.sum`, Go pins, or `go.mod requires go` CI failures |
| [create-pr](skills/create-pr/SKILL.md) | Opening PRs, fork `/allow` behavior |
| [debug-e2e-tests](skills/debug-e2e-tests/SKILL.md) | Investigating failed e2e / OpenShift CI runs |
| [update-upstream-deps](skills/update-upstream-deps/SKILL.md) | Bumping upstream SHAs or editing `upstream-kustomizations/` (triggers manifest rebuild) |
| [local-dev-setup](skills/local-dev-setup/SKILL.md) | Local Kind / dev environment |
