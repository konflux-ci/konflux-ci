---
title: "Troubleshooting"
linkTitle: "Troubleshooting"
weight: 7
description: "Solutions to common issues encountered when installing and running Konflux locally."
---

## Installation issues

### Port 5000 conflict (macOS)

Port 5000 is used by macOS AirPlay Receiver.

**Option 1:** Disable AirPlay Receiver — System Settings → General → AirDrop & Handoff → AirPlay Receiver → Off

**Option 2:** Use a different port. In `scripts/deploy-local.env`, set:

```bash
REGISTRY_HOST_PORT=5001
```

### Insufficient memory — Podman on macOS

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

### PID limit issues — Podman on Linux

If Tekton pipelines fail with "cannot fork" errors, increase the PID limit:

```bash
podman update --pids-limit 8192 konflux-control-plane
```

### Too many open files

Increase `inotify` limits temporarily (the `deploy-local.sh` script does this automatically,
but if you hit the issue on a restarted host):

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

### Docker Hub rate limits

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

### `unknown field "replacements"` error

If you see `error: json: unknown field "replacements"` while running setup scripts,
your `kubectl` is outdated. Install the latest version:
https://kubernetes.io/docs/tasks/tools/#kubectl

### Unable to bind PVCs

If the cluster cannot fulfill volume claims, `deploy-deps.sh` fails with:

```bash
error: timed out waiting for the condition on persistentvolumeclaims/test-pvc
... Test PVC unable to bind on default storage class
```

If using Kind, try [restarting the cluster](#restarting-the-cluster-after-host-reboot).
Otherwise, ensure that PVCs can be allocated on the cluster's default storage class.

### Operator not starting

```bash
kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager
kubectl get crds | grep konflux
```

### Components not deploying

```bash
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq
kubectl get events -n konflux-operator --sort-by='.lastTimestamp'
```

### Dex not starting

Check Dex logs:

```bash
kubectl logs -n konflux-ui deployment/dex
```

Verify the Dex configuration:

```bash
kubectl get configmap -n konflux-ui dex -o yaml
```

### Secrets not found

Verify secrets exist in the correct namespaces:

```bash
kubectl get secrets -n pipelines-as-code
kubectl get secrets -n build-service
kubectl get secrets -n integration-service
```

If any are missing, recreate them — see [GitHub Application Secrets]({{< relref "github-secrets" >}}).

---

## Runtime issues

### UI not accessible (https://localhost:9443)

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

### Restarting the cluster after host reboot

After a host reboot or sleep/wake cycle, restart the Kind container:

```bash
podman restart konflux-control-plane   # or: docker restart konflux-control-plane
```

Then re-apply the inotify limits:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

It may take a few minutes for the UI to become available again.

### Pipelines not triggering on PRs

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

### PR fails when webhook secret was not added

If a webhook secret was not added when creating the GitHub App, PR pipelines will fail
with:

```
There was an issue validating the commit: "could not validate payload, check your
webhook secret?: no signature has been detected, for security reason we are not
allowing webhooks that has no secret"
```

Go to your GitHub App settings and verify a webhook secret is configured. See
[Pipelines as Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup)
for instructions.

### Unable to create Application with Component using the Konflux UI

If you see a `404 Not Found` error when trying to create an Application with a Component
via the Konflux UI, the image-controller was most likely not installed or the Quay token
secret is missing.

Enable image-controller in your Konflux CR and create the `quaytoken` secret as
described in the [Installing on Kubernetes]({{< relref "install-kubernetes" >}}#quayio-auto-provisioning-optional)
guide, then try again.

### Release fails

If a release is triggered but then fails, check the logs of the pods in the managed
namespace (e.g. `managed-ns2`):

```bash
kubectl get pods -n managed-ns2
```

Check the logs of any pods with `Error` status:

```bash
kubectl logs -n managed-ns2 <pod-name>
```

#### Unfinished string at EOF

Logs contain:

```
parse error: Unfinished string at EOF at line 2, column 0
```

Verify that you provided a value for the `repository` field in
`test/resources/demo-users/user/sample-components/managed-ns2/rpa.yaml`, then redeploy:

```bash
kubectl apply -k ./test/resources/demo-users/user/sample-components/managed-ns2
```

#### 400 Bad Request

Logs contain:

```
Error: PUT https://quay.io/...: unexpected status code 400 Bad Request
```

Verify that you created a registry push secret for the managed namespace (`managed-ns2`).
See [Registry Configuration](https://github.com/konflux-ci/konflux-ci/blob/main/docs/registry-configuration.md#configuring-a-push-secret-for-the-release-pipeline)
for instructions.
