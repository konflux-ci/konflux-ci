<!-- Line count is capped; see MAX_AGENTS_MD_LINES in .github/workflows/validate-agents-md.yaml -->
# AGENTS.md

Konflux CI platform operator (Kubebuilder v4, Operator SDK, controller-runtime).

**Three Go modules** (no root `go.mod`): `operator/`, `test/go-tests/`, `operator/docs/`. Always `cd operator` before `make`, `go test`, or lint.

Layout: `operator/` (controllers, `config/`, `upstream-kustomizations/`, embedded `pkg/manifests/`), `dependencies/`, `integrations/`, `test/go-tests/`, `scripts/`.

## Session hygiene

- One atomic change per session. After a plan or a finished change, `/compact` or start a new chat; do not continue into review, tests, and PR in the same long thread.
- Do not paste log walls; write them to a file and point at it.
- Spawn short-lived subagents for isolated implement, test, or review work.
- Read only the matching `skills/<name>/SKILL.md`; do not load unrelated skills.
- Prefer existing scripts over re-deriving commands (see `skills/*/scripts/` and `operator/pkg/manifests/`).

## Code style

- Shell: `set -euo pipefail`, quote variables. Host-run scripts (deploy, CLI helpers, ConfigMap scripts users run locally) must work on Linux and macOS: POSIX constructs, no GNU-only flags, test GNU and BSD `sed`/`date`/`readlink`.
- Go tests: match the file, then neighbors; default Ginkgo/Gomega only if no established style. Do not convert packages to another framework. Ginkgo patterns: `skills/ginkgo-testing/SKILL.md`.
- `test/go-tests/` GitHub clients: nil-safe `Get*()` getters, never dereference pointer fields.
- Kustomizations: pin exact SHAs, not branches.
- Markdown: `npx markdown-toc -i` if structure changes.
- This repo is upstream. Do not name downstream consumers (e.g. `infra-deployments`) in code or comments. Use "in some environments" or "by external policies".

## Operator work

From `operator/`: `make test`, `make lint`, `make manifests generate` (after API/RBAC marker changes; CI `operator-verify-generated-files` fails if stale), `make run`. Other targets (`test-e2e`, `lint-fix`, `install`, `docker-build`, `docs-serve`, ...): `operator/Makefile`. golangci-lint version is the single semver line in `operator/.golangci-lint-version` (shared with CI `operator-lint`). CI workflow names: `skills/create-pr/SKILL.md`.

After a small Go change, lint and vet only the package you edited (repo root has no `go.mod`):

1. Read `operator/.golangci-lint-version`.
2. If `operator/bin/golangci-lint-<version>` is missing, `make golangci-lint` from `operator/`.
3. `./bin/golangci-lint-<version> run <package-dir>/` and `go vet <package-dir>/`.

Upstream pins: `?ref=SHA` matching `newTag: SHA`. Pipeline bundles (digests) are Renovate-managed. The operator reconciles a `Konflux` CR to deploy services. APIs in `operator/api/v1alpha1` (`KonfluxBuildService`, `KonfluxImageController`, ...); reconcilers in `operator/internal/controller/<subservice>/`.

Controller-runtime:

- Top-down config: `Konflux` forwards spec to sub-CRs. Sub-reconcilers read their own spec; never the parent `Konflux` CR.
- Periodic work: `source.Channel` plus a Runnable ticker/broadcaster, not `RequeueAfter` (`RequeueAfter` is for convergence).
- Any `Start()` / Runnable that creates channels, tickers, or goroutines must stop them when the context is cancelled (`defer`).
- Depend on the narrowest type (e.g. `<-chan event.TypedGenericEvent[client.Object]`, not the broadcaster). Wire via `Subscribe()` in `main.go`.
- After an API write, do not `Get` the same object from the cached client; use write-path return values or carried state.
- Controller wiring checklist: `operator/docs/component-monitoring.md`.

## Testing

- Conformance: copy `test/e2e/e2e.env.template` to `test/e2e/e2e.env`, source it, then `./test/e2e/run-e2e.sh` (`test/go-tests/tests/conformance/`; shared `Framework` in `test/go-tests/pkg/framework/`). Also `skills/local-dev-setup/SKILL.md`.
- Operator envtest: `make test` from `operator/` (`operator/internal/controller/testutil/`).
- Manager-role RBAC contract (`operator/internal/rbac/`, also in `make test`): forbids unscoped `escalate`/`bind` on clusterroles and unscoped `bind` on clusterrolebindings; requires `escalate` for embedded component ClusterRoles; every named `escalate`/`bind` target must be known.
- CRD self-healing/drift tests: `skills/ginkgo-testing/SKILL.md`.

Local Kind / operator loop: `skills/local-dev-setup/SKILL.md`, `skills/dev-verify-loop/SKILL.md`.

## When relevant

Read the matching skill before acting; do not load unrelated skills.

- PR review (human- or agent-authored; skip companion-eligible MintMaker/Renovate parents): `skills/pr-review/SKILL.md` (upstream/downstream hygiene).
- `go.mod` / Go pins / `go.mod requires go` CI: `skills/go-toolchain-upgrade/SKILL.md`.
- MintMaker/Renovate companion flow; do not apply `deps-only`, `superseded-by-companion`, `pending-upstream-image`: `skills/companion-pr-review/SKILL.md`.
- Open a PR / fork `/allow`: `skills/create-pr/SKILL.md`.
- Failed e2e / Prow / GHA (title contains e2e/E2E, labels `ci` or `workflow-failure`, Prow or GHA run URLs): `skills/debug-e2e-tests/SKILL.md`.
- Upstream SHA bumps: `skills/update-upstream-deps/SKILL.md`.
- `.tekton` `deploy-konflux-its` and `konflux-e2e-tests-its` pin `taskRef.revision: main`; point at the PR ref to verify, restore `main` before merge (`.tekton/pipelines/operator-e2e/README.md`).
- `integrations/` is not covered by e2e; recommend a local-cluster check for script/version changes.
