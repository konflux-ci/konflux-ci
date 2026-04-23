# AGENTS.md

## Project Overview

Konflux CI platform operator and deployment. Main components:
- `operator/` — Kubernetes operator managing all Konflux services
- `operator/upstream-kustomizations/` — Pinned upstream component versions
- `test/go-tests/` — E2E conformance tests
- `scripts/` — Local development helpers

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

## Code Style

- Shell: `set -euo pipefail`, quote variables
- Go: Standard formatting, Ginkgo for tests
- Kustomizations: Pin exact SHAs, not branches
- Markdown: Update TOC with `npx markdown-toc -i` if structure changes

## Testing

```bash
# Kube-linter (before PR)
mkdir -p .kube-linter
find . -name "kustomization.yaml" | while read f; do
    kustomize build "$(dirname "$f")" > ".kube-linter/$(dirname "$f" | tr / -).yaml"
done
kube-linter lint .kube-linter
```

## PR Guidelines

- **Same-repo branches preferred**: E2E tests run automatically
- **Fork PRs**: Require maintainer `/allow` comment to trigger tests
- Run kube-linter before submitting
- Update TOC if markdown structure changed

## Architecture Notes

- Upstream components pinned via `?ref=SHA` + matching `newTag: SHA`
- Pipeline bundles (image digests) managed separately by Renovate
- Operator reconciles `Konflux` CR to deploy/manage all services

## Skills

Detailed guides in `skills/`:
- [create-pr](skills/create-pr/SKILL.md) — PR workflow and CI behavior
- [local-dev-setup](skills/local-dev-setup/SKILL.md) — Kind cluster deployment
- [update-upstream-deps](skills/update-upstream-deps/SKILL.md) — Bump component versions
