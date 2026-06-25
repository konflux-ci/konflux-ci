---
name: dev-verify-loop
description: Use when iteratively verifying operator changes in development — code edit, stop operator, optionally regenerate manifests or CRDs, restart operator, verify. Use when the user says "verify", "test this change", "restart the operator", or needs a stop-rebuild-run cycle.
---

# Local Dev Verification Loop

Iterative stop-rebuild-restart cycle for the Konflux operator running outside the cluster via `make run`. Each iteration: stop the operator process, optionally regenerate/install manifests, restart, and verify.

## When to Use

- Verifying operator code changes against a running cluster
- The operator is running outside the cluster via `make run` (development mode)
- You need to stop and restart the operator between iterations

**Prerequisites:** A Konflux environment is deployed and the operator is running via `make run` in a backgrounded terminal. For local Kind setup, see the `local-dev-setup` skill.

## The Loop

Each verification iteration follows these steps:

1. **Stop the operator** — kill the `make run` process (see "Stopping the Operator" below)
2. **If upstream kustomizations changed** (`operator/upstream-kustomizations/`) — rebuild the embedded manifests: `bash operator/pkg/manifests/rebuild-upstream-manifests.sh .`
3. **If API types, RBAC markers, or webhook config changed** — run `cd operator && make manifests generate`
4. **If CRD schema changed** (fields added/removed/renamed in `*_types.go`) — run `cd operator && make install`
5. **Start the operator** — run `cd operator && make run` (run in the background)
6. **Wait for startup** — poll logs for `"starting manager"` or controller registration messages
7. **Verify the change** — check logs, inspect resources, confirm expected behavior
8. **If not correct** — fix the code and go back to step 1

## Stopping the Operator

**Kill ONLY the operator process — nothing else. NEVER use `pkill`, `killall`, or any broad kill command.**

```bash
bash skills/dev-verify-loop/scripts/stop-operator.sh
```

The script finds the `go run ... ./cmd/main.go` process by its command line, kills it and its child processes (the compiled binary), and confirms everything stopped.

## When to Regenerate / Rebuild

**Rebuild upstream manifests** when files in `operator/upstream-kustomizations/` changed:

```bash
bash operator/pkg/manifests/rebuild-upstream-manifests.sh .
```

This runs `kustomize build` for each component and writes the result to `operator/pkg/manifests/<component>/manifests.yaml`. These YAML files are embedded into the operator binary via `//go:embed`, so the rebuild must happen before `make run` for changes to take effect.

**Run `make manifests generate`** from `operator/` when ANY of these changed:

- `operator/api/` — CRD types, marker comments
- RBAC markers (`//+kubebuilder:rbac:...`) in controller files
- Webhook configurations

**Run `make install`** (which applies CRDs to the cluster) when:

- CRD schema changed (fields added/removed/renamed in `*_types.go`)
- New CRD added

**Skip all of the above** when only controller logic, reconciler behavior, or non-API code changed.

## Starting the Operator

From the `operator/` directory, start `make run` as a background process:

```bash
make run
```

This is a long-running process — run it in the background. Then confirm startup by checking logs for `"starting manager"` or controller registration messages.

## Verifying the Change

Verification is context-dependent — the user will tell you what to check. Examples: run a specific test, inspect operator logs, check a resource with `kubectl get`, apply a CR, or anything else. Ask the user if they haven't specified a verification method.

Always check operator logs for errors after restart — read the terminal running `make run`.

## Quick Reference

| Step | Command | When |
|------|---------|------|
| Stop operator | `bash skills/dev-verify-loop/scripts/stop-operator.sh` | Every iteration |
| Rebuild upstream | `bash operator/pkg/manifests/rebuild-upstream-manifests.sh .` | Upstream kustomization changes |
| Regen manifests | `cd operator && make manifests generate` | API/RBAC changes |
| Install CRDs | `cd operator && make install` | CRD schema changes |
| Start operator | `cd operator && make run` (background) | Every iteration |
| Check startup | Poll logs for `"starting manager"` | After start |
| Check logs | Read the `make run` terminal file | After start |
| Konflux status | `kubectl get konflux konflux -o jsonpath='{.status.conditions}'` | When needed |

## Common Mistakes

| Mistake | Fix |
|---------|-----|
| Using `pkill` or `killall` to stop operator | Always use the specific pid from the terminal file, or the helper script |
| Editing upstream kustomizations without rebuilding | Run `bash operator/pkg/manifests/rebuild-upstream-manifests.sh .` — the YAML is embedded at compile time |
| Forgetting `make install` after CRD changes | Operator will fail with validation errors — check logs |
| Starting `make run` before previous one stopped | Kill the old process first; port conflicts cause silent failures |
| Not waiting for startup before verifying | Poll for `"starting manager"` log line |
| Running `make run` from repo root | Must run from `operator/` directory |
