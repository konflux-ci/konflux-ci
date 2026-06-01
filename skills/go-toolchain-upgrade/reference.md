# Go toolchain upgrade — reference

Paths are in **external** repos unless noted. Grep current image tags before editing.

## Dependency overview

```
konflux-ci/konflux-ci (no root go.mod)
  ├─► operator/go.mod, test/go-tests/go.mod, operator/docs/go.mod
  ├─► GitHub Actions — operator-test, operator-lint, operator-test-e2e (test/go-tests)
  ├─► deploy-konflux-on-ocp.sh → cd operator && make install (Prow; not all GA jobs)
  ├─► test/e2e/run-e2e.sh → test/go-tests
  ├─► openshift/release — konflux-ci build_root, install-operator/e2e (from: src)
  └─► legacy — redhat-appstudio/ci:e2e-test-runner ← konflux-ci/e2e-tests
        conformance-tests ← clones test/go-tests
```

**Cluster version ≠ builder version:** `__ocp420` configs test OCP 4.20 but may use
`rhel-9-release-golang-1.26-openshift-5.0` for `build_root`.

## openshift/release — konflux-ci/konflux-ci

| Area | Update |
|------|--------|
| `ci-operator/config/konflux-ci/konflux-ci/konflux-ci-konflux-ci-main*.yaml` | `build_root`, `base_images.e2e-test-runner` |
| `ci-operator/step-registry/konflux-ci/install-operator/`, `e2e-tests/` | `from: src` (uses `build_root`) |
| `ci-operator/config/konflux-ci/community-catalog/` | separate catalog jobs |

Jobs: `konflux-e2e-v420`, `konflux-e2e-v420-optional`, `konflux-e2e-v420-arm64-optional`.

No `golang-1.26-openshift-4.21` tag may exist—use a newer stream (e.g. `1.26-openshift-5.0`)
while keeping `releases.*.version: "4.20"`.

```bash
cd openshift/release
make ci-operator-prowgen WHAT='--config-dir ci-operator/config/konflux-ci/konflux-ci'
```

## openshift/release — infra-deployments & legacy

| Area | Notes |
|------|--------|
| `ci-operator/config/redhat-appstudio/infra-deployments/redhat-appstudio-infra-deployments-main.yaml` | `appstudio-e2e-tests`; `e2e-test-runner` → `redhat-appstudio/ci` |
| `ci-operator/step-registry/redhat-appstudio/conformance-tests/` | `from: e2e-test-runner` |
| `ci-operator/step-registry/konflux-ci/install-konflux/` | `from: e2e-test-runner` |
| `ci-operator/config/konflux-ci/e2e-tests/konflux-ci-e2e-tests-main.yaml` | builds/promotes `e2e-test-runner` |

## konflux-ci/e2e-tests

Rebuild when `test/go-tests` minimum Go rises—image is `redhat-appstudio/ci:e2e-test-runner`.

## infra-deployments docs

- `components/konflux-operator/ci/openshift-overlay-e2e/README.md` — overlay step images
- `docs/temp/operator-overlay-openshift-ci-e2e-notes.md` — `e2e-test-runner` vs `from: src`

Legacy `appstudio-e2e-tests`: `konflux-ci-install-konflux` + `redhat-appstudio-conformance-tests`.

## PR template (significant only)

```markdown
## Go toolchain impact
**New minimum Go:**
### konflux-ci/konflux-ci
- Modules touched:
### openshift/release
- [ ] build_root / golang tags:
- [ ] Rehearse e2e:
### infra-deployments / legacy
- [ ] e2e-test-runner / overlay images:
### Merge order
```

## Grep helpers

Use **`rg`** ([ripgrep](https://github.com/BurntSushi/ripgrep)) or `grep -r` from the repo root.

```bash
# openshift/release
rg 'golang-1\.' ci-operator/config/konflux-ci/
rg 'golang-1\.' ci-operator/config/redhat-appstudio/infra-deployments/
rg 'e2e-test-runner|build_root' ci-operator/config/konflux-ci/konflux-ci/
rg 'from: e2e-test-runner|from: src' ci-operator/step-registry/konflux-ci/
rg 'from: e2e-test-runner' ci-operator/step-registry/redhat-appstudio/

# konflux-ci/konflux-ci
rg '^go 1\.' operator/go.mod test/go-tests/go.mod operator/docs/go.mod
rg 'GOTOOLCHAIN|golang|go version' deploy-konflux-on-ocp.sh test/e2e/run-e2e.sh .github .tekton
```

## CI log signatures

| Log | Likely cause |
|-----|----------------|
| `go.mod requires go >= 1.26` / `running go 1.25` | Stale Prow `build_root` or runner |
| `GOTOOLCHAIN=auto` but env is `local` | RHEL builder; bump image |
| infra-deployments conformance only fails | Stale `e2e-test-runner` |
