Troubleshooting Common Issues
===

<!-- toc -->

- [Using Podman with Kind while also having Docker Installed](#using-podman-with-kind-while-also-having-docker-installed)
- [Unknown Field "replacements"](#unknown-field-replacements)
- [Restarting the Cluster](#restarting-the-cluster)
- [Unable to Create Application with Component Using the Konflux UI](#unable-to-create-application-with-component-using-the-konflux-ui)
- [PR changes are not Triggering Pipelines](#pr-changes-are-not-triggering-pipelines)
- [PR Fails when Webhook Secret was not Added](#pr-fails-when-webhook-secret-was-not-added)
- [Setup Scripts Fail or Pipeline Execution Stuck or Fails](#setup-scripts-fail-or-pipeline-execution-stuck-or-fails)
  * [Running out of Resources](#running-out-of-resources)
    + [For Podman Users (macOS)](#for-podman-users-macos)
    + [For Podman Users (Linux)](#for-podman-users-linux)
    + [For All Platforms](#for-all-platforms)
  * [Unable to Bind PVCs](#unable-to-bind-pvcs)
  * [Release Fails](#release-fails)
    + [Common Release Issues](#common-release-issues)
      - [Unfinished string at EOF](#unfinished-string-at-eof)
      - [400 Bad Request](#400-bad-request)
- [Docker Hub Rate Limits](#docker-hub-rate-limits)

<!-- tocstop -->

# Using Podman with Kind while also having Docker Installed

If you have docker installed, Kind will try to use it by default. If you prefer,
you can set it to use Podman instead.

:gear: Create the cluster on Podman:

```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --name konflux --config kind-config.yaml
```

# Unknown Field "replacements"

If you get the following error: `error: json: unknown field "replacements"`, while
executing any of the setup scripts, you will need to update your `kubectl`.

:gear: Install the latest kubectl: https://kubernetes.io/docs/tasks/tools/#kubectl

# Restarting the Cluster

When running on Kind, and the host has been restarted or taken out of sleep mode, or if
you're unable to troubleshoot some other issues, you may need to restart the container
on which the Kind cluster runs.

If you do that, you'd have to, once more, **increase the PID limit** for that container
and the **open files limit** for the host (if the host was restarted). If using
`deploy-local.sh`, these are handled automatically. For manual setup, see the
[cluster setup instructions](../README.md#bootstrapping-the-cluster).

:gear: Restart the container (if using Docker, replace `podman` with `docker`):

```bash
podman restart konflux-control-plane
```

:gear: Increase the [PID limit](../README.md#bootstrapping-the-cluster).

**Note:** It might take a few minutes for the UI to become available once the container
is restarted.

# Unable to Create Application with Component Using the Konflux UI

If you see error `404 Not Found` when trying to create an Application with a Component
using the Konflux UI, this is most probably because image-controller was not properly
installed before trying to onboard the application. Refer to the Quay.io application
[setup step](../README.md#option-1-onboard-application-with-the-konflux-ui) and try
again.

# PR changes are not Triggering Pipelines

Follow this procedure if you create a PR or make changes to a PR and a pipeline is not
triggered:

1. :gear: Confirm that events were logged to the smee channel. if not, verify your steps
   for setting up the GitHub app and installing the app to your fork repository.

2. Confirm that events are being relayed by the smee client:

   :gear: List the pods under the `smee-client` namespace and check the logs of the pod
   on the namespace. Those should have mentions of the channel events being forwarded to
   pipelines-as-code.

```bash
kubectl get pods -n smee-client
kubectl logs -n smee-client gosmee-client-<some-id>
```

3. :gear: If the pod is not there or the logs do not include the mentioned entries,
   confirm you properly set the **smee channel** on the smee-client manifest and that
   you deploy the manifest to your cluster.

```bash
kubectl delete -f ./smee/smee-client.yaml
<fix the manifests>
kubectl create -f ./smee/smee-client.yaml
```

**Note:** if the host machine goes to sleep mode, the smee client might stop responding
to events on the smee channel, once the host is up again. This can be addressed by
deleting the client pod and waiting for it to be recreated.

:gear: Delete the smee client:

```bash
kubectl get pods -n smee-client
kubectl delete pods -n smee-client gosmee-client-<some-id>
```

4. Check the pipelines-as-code **controller** logs to see that events are being properly
   linked to the Repository resource. If you see log entries mentioning a repository
   resource cannot be found, compare the repository mentioned on the logs to the one
   deployed when creating the application and component resources. Fix the Repository
   resource manifest and redeploy it.

   **Note:** this should only be relevant if the application was onboarded manually
   (i.e. not using the Konflux UI).

   :gear: Identify the pod and examine the logs:

```bash
kubectl get pods -n pipelines-as-code
kubectl logs -n pipelines-as-code pipelines-as-code-controller-<some-id>
<fix the manifests>
kubectl apply -f ./test/resources/demo-users/user/ns2/application-and-component.yaml
```

5. If the pipelines-as-code logs mention secret `pipelines-as-code-secret` is
   missing/malformed, make sure you created the secret for the GitHub app, providing
   values for fields `github-private-key`, `github-application-id` and `webhook.secret`
   for the app your created.

   :gear: If the secret needs to be fixed, delete it (see command below) and deploy it
   once more based on the Pipelines as Code
   [instructions](../README.md#enable-pipelines-triggering-via-webhooks).

```bash
kubectl delete secret pipelines-as-code-secret -n pipelines-as-code
```

6. :gear: On the PR page, type `/retest` on the comment box and post the comment.
   Observe the behavior once more.

# PR Fails when Webhook Secret was not Added

If a webhook secret is not added when creating a Github App, PR pipelines will fail
and the following error will show up in the PR logs:

```
There was an issue validating the commit: "could not validate payload, check your webhook secret?: no signature has been detected, for security reason we are not allowing webhooks that has no secret"
```

To resolve this issue, go to the Github App you created and check Webhook Secret is
added there. Please refer to 
[this document](https://pipelinesascode.com/docs/install/github_apps/#manual-setup)
on how to create webhook secret.

# Setup Scripts Fail or Pipeline Execution Stuck or Fails

## Running out of Resources

If setup scripts fail or pipelines are stuck or tend to fail at relatively random
stages, it might be that the cluster is running out of resources.

That could be:

* Open files limit.
* Open threads limit.
* Cluster running out of memory.

The symptoms may include:

* Setup scripts fail.
* Pipelines are triggered, but seem stuck and listing the pods on the user namespace
  (e.g. running `kubectl get pods -n user-ns2`) shows pods stuck in pending for a long
  time. Checking the detailed output of pods with pending status `kubectl describe
  pods/<pod name with pending status> -n user-ns2`, shows following error:
  ```
  Warning  FailedScheduling  5m59s  default-scheduler  running PreBind plugin "VolumeBinding": binding volumes: context deadline exceeded
  ```
* Pipelines fail at inconsistent stages.

### For Podman Users (macOS)

On macOS, Podman runs in a virtual machine. Check and configure VM resources:

**Check Podman machine memory:**

```bash
podman machine inspect | grep Memory
```

**If insufficient, create new machine with more resources:**

```bash
podman machine stop
podman machine init --memory 20480 --cpus 8 --rootful konflux-large
podman machine start konflux-large
```

**Configure deployment script to use the new machine** (in `scripts/deploy-local.env`):

```bash
PODMAN_MACHINE_NAME="konflux-large"
```

### For Podman Users (Linux)

On Linux, Podman runs natively without a VM. Ensure your system has sufficient
resources (8GB+ RAM recommended). No Podman machine configuration is needed.

**PID limit issues** (Tekton pipelines fail with "cannot fork" errors):

The setup script automatically increases PID limits. If issues persist:

```bash
podman update --pids-limit 8192 konflux-control-plane
```

**Performance tuning:**

If builds are slow or UI is sluggish, you can reduce resource requirements by lowering replica counts in your Konflux CR:

```yaml
spec:
  ui:
    spec:
      proxy:
        replicas: 1  # Instead of 2-3
```

### For All Platforms

:gear: For general mitigation steps, consult the notes at the top of the
[cluster setup instructions](../README.md#bootstrapping-the-cluster).

:gear: As last resort, you could restart the container running the cluster node. To do
that, refer to the instructions for [restarting the cluster](#restarting-the-cluster).

## Unable to Bind PVCs
The `deploy-deps.sh` script includes a check to verify whether PVCs on the default
storage class can be bind. If volume claims are unable to be fulfilled, the script will
fail, displaying:

```bash
error: timed out waiting for the condition on persistentvolumeclaims/test-pvc
... Test PVC unable to bind on default storage class
```

:gear: If using Kind, try to [restart the container](#restarting-the-cluster).

:gear: Otherwise, ensure that PVCs (Persistent Volume Claims) can be allocated for the
cluster's default storage class.

## Release Fails

If a release is triggered, but then fails, check the logs of the pods on the managed
namespace (e.g. `managed-ns2`).

:gear: List the pods and look for ones with status `Error`:

```bash
kubectl get pods -n managed-ns2
```

:gear: Check the logs of the pods with status `Error`:

```bash
kubectl logs -n managed-ns2 managed-7lxdn-push-snapshot-pod
```

Compare the logs to the [common release issues](#common-release-issues) below.

:gear: Once you addressed the issue, create a PR and merge it, or directly push a change
to the main branch, so that the on-push pipeline is triggered.

### Common Release Issues

#### Unfinished string at EOF

The logs contain statements similar to this:

```bash
++ jq -r .containerImage
parse error: Unfinished string at EOF at line 2, column 0
```

**Solution**:

:gear: Verify that you provided a value to the `repository` field inside
the [rpa.yaml file](../test/resources/demo-users/user/sample-components/managed-ns2/rpa.yaml).

:gear: Complete the value and redeploy the manifest:

```bash
kubectl apply -k ./test/resources/demo-users/user/sample-components/managed-ns2
```

#### 400 Bad Request

The logs contain statements similar to this:

```bash
Error: PUT https://quay.io/...: unexpected status code 400 Bad Request: <html>
<head><title>400 Bad Request</title></head>
<body>
<center><h1>400 Bad Request</h1></center>
</body>
</html>

main.go:74: error during command execution: PUT https://quay.io/...: unexpected status code 400 Bad Request: <html>
<head><title>400 Bad Request</title></head>
<body>
<center><h1>400 Bad Request</h1></center>
</body>
</html>
```

**Solution**:

:gear: verify that you
[created the registry secret](./registry-configuration.md#configuring-a-push-secret-for-the-release-pipeline)
also for the managed namespace.

# Docker Hub Rate Limits

Docker Hub enforces rate limits on image pulls. If you encounter script failures related
to rate limiting, you can pre-load images locally to avoid those issues.

:gear: Check for rate limit events:

```bash
kubectl get events -A | grep toomanyrequests
```

If the command returns results, you are hitting Docker Hub rate limits.

:gear: Pre-load the affected images into your Kind cluster to avoid pulling from
Docker Hub:

```bash
podman login docker.io
podman pull ghcr.io/project-zot/zot:v2.1.13
kind load docker-image ghcr.io/project-zot/zot:v2.1.13 --name konflux
```

:gear: Continue with normal deployment after pre-loading images.
