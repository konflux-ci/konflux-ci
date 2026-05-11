---
name: local-dev-setup
description: Deploy Konflux on a local Kind cluster. Supports development mode (operator outside cluster for fast iteration) and preview mode (operator deployed in-cluster from release/build). Use when the user wants to start, restart, or recreate a local Konflux environment, run the operator locally, install integrations like sigstore or quay, or troubleshoot local deployment.
---

# Local Development Setup

Deploy Konflux on a local Kind cluster. Two modes are available:

- **Development** (`OPERATOR_INSTALL_METHOD=none`) — operator runs on the host for fast rebuild-restart cycles. **This is the default mode for this skill.**
- **Preview** (`release`, `build`, or `local`) — operator runs in-cluster; `deploy-local.sh` handles everything end-to-end.

If the user doesn't specify, use **development** mode. Ask only if their intent is ambiguous.

Full documentation: `operator/docs/content/docs/installation/install-local.md`

## Prerequisites

- `kind`, `kubectl`, `podman` (or `docker`)
- `helm` (only for sigstore integration)
- A configured `scripts/deploy-local.env` file (see "Env File" section)

## Env File

The env file at `scripts/deploy-local.env` must exist before running. If it doesn't, copy the template:

```bash
cp scripts/deploy-local.env.template scripts/deploy-local.env
```

Required secrets (must already be populated by the user — never generate or guess these):

| Variable | Description |
|----------|-------------|
| `GITHUB_APP_ID` | Numeric GitHub App ID |
| `GITHUB_PRIVATE_KEY` or `GITHUB_PRIVATE_KEY_PATH` | PEM key content or path to .pem file |
| `WEBHOOK_SECRET` | GitHub webhook secret |

GitHub App setup guide: `operator/docs/content/docs/guides/github-secrets.md`

### OPERATOR_INSTALL_METHOD values

| Value | Mode | Description |
|-------|------|-------------|
| `none` | Development | Operator runs on host via `make run` |
| `release` | Preview | Install from latest GitHub release |
| `build` | Preview | Build operator image from source |
| `local` | Preview | Kustomize from current checkout |

Set the appropriate value in `deploy-local.env` before running. If the file already has a different value, change it using StrReplace.

## (Optional) Delete and Recreate the Kind Cluster

If the user asks to recreate the cluster, or if the cluster is in a broken state, delete it first:

```bash
kind delete cluster --name konflux
```

The containerd image cache (`~/.cache/konflux-ci/containerd-cache`) persists across cluster deletions when `ENABLE_IMAGE_CACHE=1`, making recreation much faster. **Do not clear the cache** unless the user explicitly asks or there is a specific issue (e.g. corrupted images, Kubernetes version change):

```bash
rm -rf ~/.cache/konflux-ci/containerd-cache
```

---

## Development Mode (operator outside cluster)

Use this when the user is developing the operator and needs fast iteration.

### Checklist

Track progress with TodoWrite:

```
- [ ] Ensure scripts/deploy-local.env exists and has OPERATOR_INSTALL_METHOD=none
- [ ] (Optional) Delete existing Kind cluster
- [ ] Run deploy-local.sh to create cluster and deploy dependencies
- [ ] Install CRDs: cd operator && make install
- [ ] Run operator locally: cd operator && make run
- [ ] Apply the Konflux CR
- [ ] Wait for Konflux to become ready
- [ ] (Optional) Install integrations
```

### Step 1: Set OPERATOR_INSTALL_METHOD=none

Verify `scripts/deploy-local.env` has:

```
OPERATOR_INSTALL_METHOD=none
```

### Step 2: Deploy Cluster and Dependencies

```bash
./scripts/deploy-local.sh
```

This creates the Kind cluster and installs dependencies (Tekton, cert-manager, etc.) but skips operator installation. Takes several minutes. Use `block_until_ms: 0` and poll the terminal for "Deployment Complete!" or an error.

### Step 3: Install CRDs and Run the Operator

From the `operator/` directory:

```bash
cd operator && make install
```

Then start the operator — this is a long-running process, background it immediately:

```bash
make run
```

Use `block_until_ms: 0` for `make run` since it runs indefinitely. Poll the terminal to confirm startup (look for "Starting workers" or controller log lines).

### Step 4: Apply the Konflux CR and Wait

In a **separate** shell (the operator terminal is occupied), apply the CR using the shim script. It uses `scripts/resolve-konflux-cr.sh` to pick the right CR (defaults to the base CR; auto-selects `konflux-e2e.yaml` when Quay credentials are set).

**Run these commands from the repository root** (use `working_directory` parameter pointing to the repo root, since the previous step leaves the shell in `operator/`):

```bash
bash skills/local-dev-setup/scripts/apply-konflux-cr.sh
```

To use a specific CR file:

```bash
bash skills/local-dev-setup/scripts/apply-konflux-cr.sh operator/config/samples/konflux_v1alpha1_konflux.yaml
```

Then wait for Konflux to become ready:

```bash
bash skills/local-dev-setup/scripts/wait-for-konflux.sh
```

Both scripts are in the command allowlist and run without user confirmation.

### Restarting the Operator

When the user modifies operator code and wants to restart:

1. Stop the running `make run` process (kill it via the terminal pid)
2. Run `make run` again from `operator/`

No need to recreate the cluster or redeploy dependencies. If API types changed, run `make install` before `make run`.

---

## Preview Mode (operator in cluster)

Use this when the user wants a fully self-contained Konflux instance without running the operator on the host.

### Checklist

Track progress with TodoWrite:

```
- [ ] Ensure scripts/deploy-local.env exists with desired OPERATOR_INSTALL_METHOD (release/build/local)
- [ ] (Optional) Delete existing Kind cluster
- [ ] Run deploy-local.sh — handles everything
- [ ] Wait for Konflux to become ready
- [ ] (Optional) Install integrations
```

### Step 1: Set OPERATOR_INSTALL_METHOD

Set the desired method in `scripts/deploy-local.env`:

- `release` — latest GitHub release (simplest, recommended for preview)
- `build` — builds the operator image from the current checkout
- `local` — applies kustomize manifests from the current checkout with the latest released image

### Step 2: Run deploy-local.sh

```bash
./scripts/deploy-local.sh
```

The script handles everything: Kind cluster, dependencies, operator deployment, CR application, secrets, and readiness wait. Takes several minutes. Use `block_until_ms: 0` and poll for "Deployment Complete!" or an error.

For Podman on macOS:

```bash
export KIND_EXPERIMENTAL_PROVIDER=podman
./scripts/deploy-local.sh
```

---

## (Optional) Install Integrations

Integrations live in `integrations/`. Each has its own install script. Install them **after** Konflux is ready unless the integration docs say otherwise.

### Sigstore

Deploys Fulcio, Rekor, CT Log, and Trillian for keyless signing. Requires `helm`.

```bash
./integrations/sigstore/install.sh
```

Patches the Konflux CR and Tekton Chains config automatically. Takes several minutes (Helm install with `--wait --timeout 15m`). Use `block_until_ms: 0` and poll.

### Quay (Self-Hosted Image Controller)

Configures image-controller to use the self-hosted Quay deployed by `deploy-deps.sh`. Only works if Quay was deployed (requires `SKIP_QUAY=false` when running deploy-deps).

```bash
./integrations/quay/configure-image-controller.sh
```

For external Quay (quay.io), set `QUAY_TOKEN` and `QUAY_ORGANIZATION` in `deploy-local.env` and use a CR that enables image-controller:

```bash
KONFLUX_CR=operator/config/samples/konflux-e2e.yaml ./scripts/deploy-local.sh
```

### Adding New Integrations

If the user asks to install an integration not listed here, look for an install script in the relevant `integrations/<name>/` directory and run it.

## Verification

- Operator logs: in dev mode, check the terminal running `make run`; in preview mode, `kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager`
- Konflux status: `kubectl get konflux konflux -o jsonpath='{.status.conditions}'`
- UI: https://localhost:9443
- Credentials: `user1@konflux.dev` / `password` (also `user2@konflux.dev`)
- Registry: `localhost:5001` (if `ENABLE_REGISTRY_PORT=1`)

## Key Files

| File | Purpose |
|------|---------|
| `scripts/deploy-local.sh` | Main deploy script |
| `scripts/deploy-local.env.template` | Config template |
| `scripts/setup-kind-local-cluster.sh` | Kind cluster setup |
| `deploy-deps.sh` | Dependency installation |
| `scripts/deploy-secrets.sh` | Secret creation |

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `make run` fails with CRD errors | Run `make install` first, then `make run` |
| Cluster exists but is broken | Delete and recreate: `kind delete cluster --name konflux && ./scripts/deploy-local.sh` |
| Port 5001 in use | Set `REGISTRY_HOST_PORT` to another port in `deploy-local.env`, or set `ENABLE_REGISTRY_PORT=0` |
| Operator panics on start | Check if secrets exist: `kubectl get secrets -n konflux-build-service` — if missing, re-run `deploy-local.sh` |
| Konflux CR not becoming Ready | `kubectl get konflux konflux -o yaml` and inspect `.status.conditions` |
