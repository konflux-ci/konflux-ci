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
- **`make lint`** / **`make lint-fix`** — golangci-lint
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

- Shell: `set -euo pipefail`, quote variables
- Go: Standard formatting, Ginkgo for tests, Gomega for assertions/matchers (all test types: unit, functional, e2e)
- Kustomizations: Pin exact SHAs, not branches
- Markdown: Update TOC with `npx markdown-toc -i` if structure changes

## Testing

**Two distinct test suites:**

1. **Platform conformance** (`test/go-tests/tests/conformance/`) — end-to-end tests against a deployed Konflux instance, run via `test/e2e/run-e2e.sh`. Uses Ginkgo/Gomega with a shared `Framework` in `test/go-tests/pkg/framework/`.
2. **Operator unit/integration** (`operator/`) — controller tests using controller-runtime **envtest** (no real cluster needed). Shared test utilities in `operator/internal/controller/testutil/`. Run via `make test` from `operator/`.

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

## PR Guidelines

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
