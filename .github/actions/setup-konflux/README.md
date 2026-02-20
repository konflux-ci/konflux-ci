# Setup Konflux Action

This GitHub Action deploys a ephemeral Konflux environment on a Kind cluster for testing and development purposes. It wraps the `scripts/deploy-local.sh` script to provide a simple, reusable workflow step.

## Usage

```yaml
steps:
  - uses: actions/checkout@v4
  
  - name: Deploy Konflux
    uses: konflux-ci/konflux-ci/.github/actions/setup-konflux@main
    with:
      github_app_id: ${{ secrets.KONFLUX_GITHUB_APP_ID }}
      webhook_secret: ${{ secrets.KONFLUX_WEBHOOK_SECRET }}
      github_private_key: ${{ secrets.KONFLUX_GITHUB_PRIVATE_KEY }}
      # Optional: To enable image controller
      # quay_token: ${{ secrets.QUAY_ROBOT_TOKEN }}
      # quay_organization: my-quay-org
```

## Inputs

| Input | Description | Required | Default |
| :--- | :--- | :--- | :--- |
| `github_app_id` | GitHub App ID for Konflux integration | Yes | |
| `webhook_secret` | Webhook verification secret | Yes | |
| `github_private_key` | GitHub App Private Key | Yes | |
| `quay_token` | Quay.io robot token (e.g. key:token) | No | |
| `quay_organization` | Quay.io organization | No | |
| `konflux_cr_path` | Path to custom Konflux CR file | No | `operator/config/samples/konflux_v1alpha1_konflux.yaml` |
| `install_method` | Installation method (`release`, `build`, `local`) | No | `release` |

## Prerequisites

The runner must be a Linux environment capable of running Docker (e.g., `ubuntu-latest`). The action will automatically install `kind` if it is not present.
