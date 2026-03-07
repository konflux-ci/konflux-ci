---
title: "Troubleshooting"
linkTitle: "Troubleshooting"
weight: 5
description: "Solutions to common issues encountered when installing and running Konflux locally."
---

## UI not accessible (https://localhost:9443)

Verify that your Konflux CR includes the NodePort configuration. The default sample CR
sets `httpsPort: 30011`, which Kind maps to host port 9443 via `kind-config.yaml`.

If the configuration is missing, patch the Konflux CR:

```bash
kubectl patch konflux konflux --type=merge -p '
spec:
  ui:
    spec:
      ingress:
        nodePortService:
          httpsPort: 30011
'
```

{{< alert color="info" >}}
Always use <code>https://</code> — typing <code>localhost:9443</code> without the scheme defaults
to HTTP and will fail to load.
{{< /alert >}}

## Port 5000 conflict (macOS)

Port 5000 is used by macOS AirPlay Receiver.

**Option 1:** Disable AirPlay Receiver — System Settings → General → AirDrop & Handoff → AirPlay Receiver → Off

**Option 2:** Use a different port. In `scripts/deploy-local.env`, set:

```bash
REGISTRY_HOST_PORT=5001
```

## Insufficient memory — Podman on macOS

On macOS, Podman runs in a virtual machine. Check the VM memory:

```bash
podman machine inspect | grep Memory
```

If insufficient, create a new machine with more resources:

```bash
podman machine stop
podman machine init --memory 20480 --cpus 8 --rootful konflux-large
podman machine start konflux-large
```

Then configure the deployment script to use it (in `scripts/deploy-local.env`):

```bash
PODMAN_MACHINE_NAME="konflux-large"
```

## PID limit issues — Podman on Linux

If Tekton pipelines fail with "cannot fork" errors, increase the PID limit:

```bash
podman update --pids-limit 8192 konflux-control-plane
```

## Too many open files

Increase `inotify` limits temporarily (the `deploy-local.sh` script does this automatically,
but if you hit the issue on a restarted host):

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

## Restarting the cluster after host reboot

After a host reboot or sleep/wake cycle, restart the Kind container:

```bash
podman restart konflux-control-plane   # or: docker restart konflux-control-plane
```

Then re-apply the inotify limits above. It may take a few minutes for the UI to become
available again.

## Docker Hub rate limits

If deployment fails with `toomanyrequests` errors, check for rate limit events:

```bash
kubectl get events -A | grep toomanyrequests
```

Pre-load the affected images into Kind to bypass Docker Hub:

```bash
podman login docker.io
podman pull ghcr.io/project-zot/zot:v2.1.13
kind load docker-image ghcr.io/project-zot/zot:v2.1.13 --name konflux
```

## `unknown field "replacements"` error

If you see `error: json: unknown field "replacements"` while running setup scripts,
your `kubectl` is outdated. Install the latest version:
https://kubernetes.io/docs/tasks/tools/#kubectl

## Pipelines not triggering on PRs

1. Check that events are being logged to your smee channel.
2. Check the smee client pod logs:

```bash
kubectl get pods -n smee-client
kubectl logs -n smee-client gosmee-client-<pod-id>
```

3. If the smee client pod is missing or not forwarding, check the channel URL in the
   smee client manifest and redeploy it.
4. Check pipelines-as-code controller logs:

```bash
kubectl get pods -n pipelines-as-code
kubectl logs -n pipelines-as-code pipelines-as-code-controller-<pod-id>
```

5. Verify the `pipelines-as-code-secret` exists and has the correct fields:

```bash
kubectl get secret pipelines-as-code-secret -n pipelines-as-code
```

## Operator not starting

```bash
kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager
kubectl get crds | grep konflux
```

## Components not deploying

```bash
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq
kubectl get events -n konflux-operator --sort-by='.lastTimestamp'
```
