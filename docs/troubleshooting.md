# Troubleshooting Common Issues

This guide covers common problems across all Konflux deployment types.

## Deployment Issues

### Operator Not Starting

The operator fails to start or becomes unavailable after installation.

Check operator logs for errors:

```bash
kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager
```

Verify CRDs installed correctly:

```bash
kubectl get crds | grep konflux
```

Missing CRDs indicate incomplete operator installation. Run `make install` from the operator directory.

Check for resource constraints on the operator pod:

```bash
kubectl describe pod -n konflux-operator -l control-plane=controller-manager
```

### Components Not Deploying

Konflux CR is applied but components fail to deploy.

Check the Konflux status for error conditions:

```bash
kubectl get konflux konflux -o jsonpath='{.status.conditions}' | jq
```

Review operator events for detailed error messages:

```bash
kubectl get events -n konflux-operator --sort-by='.lastTimestamp'
```

Common causes include missing secrets (verify GitHub App secrets exist in required namespaces), insufficient cluster resources (check node capacity with `kubectl describe nodes`), invalid CR configuration (compare against sample configurations), or prerequisite operators not running (ensure Tekton and cert-manager are installed and ready).

### UI Not Accessible

**Kind clusters:** The UI requires NodePort configuration to be accessible from the host.

Verify NodePort configuration in your Konflux CR or KonfluxUI resource:

```yaml
ui:
  spec:
    ingress:
      nodePortService:
        httpsPort: 30011
```

Patch the KonfluxUI CR if configuration is missing:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  ingress:
    nodePortService:
      httpsPort: 30011
'
```

Verify the Kind configuration includes the port mapping. Check `kind-config-arm64.yaml`:

```yaml
extraPortMappings:
  - containerPort: 30011
    hostPort: 9443
    protocol: TCP
```

Check that you're using `https://localhost:9443` not `http://localhost:9443`. Browsers default to HTTP when you type `localhost:9443` without the protocol, which fails.

**Production clusters:** Verify ingress configuration matches your ingress controller.

Check ingress resource status:

```bash
kubectl get ingress -n konflux-ui
kubectl describe ingress -n konflux-ui
```

Ensure your ingress controller is running and the host resolves correctly.

### Insufficient Resources

Pods fail to schedule or remain in pending state due to insufficient cluster resources.

Check node resource availability:

```bash
kubectl top nodes
kubectl describe nodes | grep -A 5 "Allocated resources"
```

List pods stuck in pending state:

```bash
kubectl get pods -A | grep Pending
kubectl describe pod <pod-name> -n <namespace>
```

Look for scheduling failures in pod events. Common messages include "Insufficient memory," "Insufficient cpu," or "0/1 nodes available."

**For Kind clusters:** Increase system limits before creating the cluster:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

Increase PID limit on the Kind container:

```bash
# Podman
podman update --pids-limit 4096 konflux-control-plane

# Docker
docker update --pids-limit 4096 konflux-control-plane
```

Edit `kind-config.yaml` to reserve more memory under `kubeletExtraArgs`:

```yaml
kubeadmConfigPatches:
- |
  kind: InitConfiguration
  nodeRegistration:
    kubeletExtraArgs:
      node-labels: "ingress-ready=true"
      system-reserved: memory=12Gi
```

**For macOS users:** Verify Podman machine has sufficient memory:

```bash
podman machine inspect | grep Memory
```

See [Mac Setup Guide](mac-setup.md#insufficient-memory) for detailed memory configuration.

### PVCs Unable to Bind

The `deploy-deps.sh` script verifies PVC binding on the default storage class. Binding failures indicate storage provisioning issues.

For Kind clusters, restart the cluster container:

```bash
# Podman
podman restart konflux-control-plane

# Docker
docker restart konflux-control-plane
```

For other clusters, verify a storage provisioner is installed and the default storage class exists:

```bash
kubectl get storageclass
kubectl get pv
```

### Using Podman with Docker Installed

Kind defaults to Docker when both Docker and Podman are installed. Force Kind to use Podman:

```bash
KIND_EXPERIMENTAL_PROVIDER=podman kind create cluster --name konflux --config kind-config.yaml
```

### Unknown Field "replacements"

This error appears when kubectl is too old to support Kustomize features. Install kubectl v1.31.4 or newer:

```bash
# macOS
brew upgrade kubectl

# Linux - see https://kubernetes.io/docs/tasks/tools/#kubectl
```

## GitHub Integration Issues

### Pipelines Not Triggering

Pull request events don't trigger pipeline runs.

Verify GitHub App installation. Navigate to your GitHub App settings and confirm the app is installed on the repository. Check that webhook events are configured and the webhook URL points to your smee channel (for Kind) or cluster ingress (for production).

Confirm events reach the smee channel. For Kind clusters, navigate to your smee channel URL in a browser. After creating a PR, verify events appear (look for `pull_request` and `check_run` events).

Check smee client logs:

```bash
kubectl get pods -n smee-client
kubectl logs -n smee-client gosmee-client-<pod-id>
```

The logs should show events being forwarded to pipelines-as-code.

If the smee client is not running or not logging events, verify the smee channel URL is correctly configured:

```bash
kubectl get configmap -n smee-client gosmee-client -o yaml
```

Fix and redeploy if needed:

```bash
kubectl delete -f ./dependencies/smee/smee-client.yaml
# Fix the manifest
kubectl create -f ./dependencies/smee/smee-client.yaml
```

Examine pipelines-as-code controller logs:

```bash
kubectl get pods -n pipelines-as-code
kubectl logs -n pipelines-as-code pipelines-as-code-controller-<pod-id>
```

Look for errors related to repository matching or webhook processing.

Verify the Repository resource matches the repository in GitHub:

```bash
kubectl get repository -A
kubectl describe repository <repository-name> -n <namespace>
```

The repository URL in the Resource must match the repository URL in GitHub events.

Check for missing or incorrect secrets:

```bash
kubectl get secret pipelines-as-code-secret -n pipelines-as-code
kubectl get secret pipelines-as-code-secret -n build-service
kubectl get secret pipelines-as-code-secret -n integration-service
```

If secrets are missing or incorrect, delete and recreate them:

```bash
kubectl delete secret pipelines-as-code-secret -n pipelines-as-code

kubectl create secret generic pipelines-as-code-secret \
  -n pipelines-as-code \
  --from-file=github-private-key=/path/to/github-app.pem \
  --from-literal=github-application-id="123456" \
  --from-literal=webhook.secret="your-webhook-secret"
```

Repeat for build-service and integration-service namespaces.

Add a `/retest` comment to your PR to trigger a new build.

### Webhook Secret Missing

Pipelines fail with the error "could not validate payload, check your webhook secret?"

This indicates the GitHub App webhook secret is missing. Navigate to your GitHub App settings, verify a webhook secret is configured, then update the Kubernetes secrets with the correct value.

See the [Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup) for webhook secret generation instructions.

### Smee Client Not Responding

The smee client may stop responding after the host machine sleeps or restarts.

Delete the smee client pod to force recreation:

```bash
kubectl get pods -n smee-client
kubectl delete pod -n smee-client gosmee-client-<pod-id>
```

Kubernetes automatically creates a new pod that reconnects to the smee channel.

## Authentication Issues

### Demo Login Fails

Demo users are configured but login fails at the UI.

Verify demo users are configured in the KonfluxUI CR:

```bash
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.staticPasswords}' | jq
```

If empty, demo users are not configured. See [Demo Users Configuration](demo-users.md) for setup instructions.

Check Dex pod status and logs:

```bash
kubectl get pods -n konflux-ui -l app=dex
kubectl logs -n konflux-ui deployment/dex
```

Verify the Dex ConfigMap contains static passwords:

```bash
kubectl get configmap -n konflux-ui -o name | grep dex
kubectl get configmap <dex-configmap-name> -n konflux-ui -o yaml | grep -A 10 staticPasswords
```

If the ConfigMap doesn't match the KonfluxUI CR, the operator may not be reconciling. Check operator logs:

```bash
kubectl logs -n konflux-operator deployment/konflux-operator-controller-manager
```

### Connector Authentication Fails

GitHub OAuth or other connector authentication fails.

Verify connector configuration in the KonfluxUI CR:

```bash
kubectl get konfluxui konflux-ui -n konflux-ui -o jsonpath='{.spec.dex.config.connectors}' | jq
```

Check that client ID and client secret are correct. Verify the redirect URI matches your OAuth application configuration (should be `https://your-host/idp/callback`).

Examine Dex logs for authentication errors:

```bash
kubectl logs -n konflux-ui deployment/dex | grep -i error
```

Common issues include incorrect client credentials, misconfigured redirect URI, or network connectivity issues between Dex and the identity provider.

### Locked Out After Disabling Password DB

You disabled the password database without configuring a connector.

Re-enable the password database temporarily through a CR patch:

```bash
kubectl patch konfluxui konflux-ui -n konflux-ui --type=merge -p '
spec:
  dex:
    config:
      enablePasswordDB: true
      staticPasswords:
      - email: "admin@konflux.dev"
        username: "admin"
        userID: "temp-admin"
        hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # gitleaks:allow
'
```

Wait for Dex to restart, log in, configure a proper connector, test the connector works, then disable the password database again.

## Pipeline Execution Issues

### Pipelines Stuck or Fail Randomly

Pipelines become stuck in pending state or fail inconsistently at different stages.

This usually indicates resource exhaustion. Check inotify limits, PID limits, and available cluster memory as described in the [Insufficient Resources](#insufficient-resources) section.

For Kind clusters, verify system limits are increased and the PID limit is set on the cluster container.

For macOS users, ensure the Podman machine has sufficient memory allocated. See [Mac Setup Guide](mac-setup.md#insufficient-memory).

Monitor resource usage during pipeline execution:

```bash
kubectl top nodes
kubectl top pods -n <user-namespace>
```

### Builds Fail Due to Resource Limits

TaskRuns fail with OOMKilled status or CPU throttling.

Kyverno policies reduce resource requirements for known tasks. Verify Kyverno is running:

```bash
kubectl get pods -n kyverno
```

Check Kyverno policies:

```bash
kubectl get clusterpolicy
```

Add policies for additional resource-intensive tasks by matching pod labels. See [this example](../dependencies/kyverno/policy/pod-requests-del.yaml) for reference.

Alternatively, increase cluster resources or reduce concurrent pipeline execution.

## Application Onboarding Issues

### Unable to Create Application via UI

The UI returns "404 Not Found" when creating an application with a component.

This indicates image-controller is not properly configured. Verify image-controller is enabled in your Konflux CR:

```yaml
imageController:
  enabled: true
```

Verify the Quay token secret exists:

```bash
kubectl get secret -n image-controller quay-token
```

See [automatic repository provisioning documentation](registry-configuration.md#automatic-repository-provisioning-quayio) for image-controller configuration.

### Repository Resource Not Found

Pipelines-as-code controller logs show "repository resource cannot be found."

This occurs when the Repository resource name doesn't match the repository referenced in webhook events.

List Repository resources:

```bash
kubectl get repository -A
```

Describe the resource to see its configuration:

```bash
kubectl describe repository <name> -n <namespace>
```

Edit the Repository resource to match the repository URL in GitHub:

```bash
kubectl edit repository <name> -n <namespace>
```

Or redeploy the application-and-component manifest with the corrected URL:

```bash
kubectl apply -f ./test/resources/demo-users/user/ns2/application-and-component.yaml
```

## Release Issues

### Release Fails to Complete

A release is triggered but fails during execution.

List pods in the managed namespace to find failed pods:

```bash
kubectl get pods -n managed-ns2
```

Check logs of pods with Error status:

```bash
kubectl logs -n managed-ns2 <pod-name>
```

Common release failures include:

**Unfinished string at EOF:** The `repository` field in ReleasePlanAdmission is empty or malformed.

Verify the ReleasePlanAdmission configuration:

```bash
kubectl get releaseplanadmission -n managed-ns2 -o yaml
```

Fix the `repository` field under `mapping.components`:

```yaml
mapping:
  components:
    - name: test-component
      repository: quay.io/my-user/my-component
```

Redeploy:

```bash
kubectl apply -k ./test/resources/demo-users/user/managed-ns2
```

**400 Bad Request from registry:** The push secret is missing or incorrect in the managed namespace.

Verify the secret exists:

```bash
kubectl get secret -n managed-ns2
```

Create the push secret following [registry secret instructions](registry-configuration.md#configuring-a-push-secret-for-the-release-pipeline).

Trigger a new release by pushing a commit or merging a PR.

## Platform-Specific Issues

### macOS Port Conflicts

macOS uses port 5000 for the AirPlay Receiver service, which conflicts with the default registry port used by some container tools.

**Disable AirPlay Receiver:** Open System Settings, navigate to General → AirDrop & Handoff, and turn off AirPlay Receiver. This frees port 5000 for other uses.

**Use Alternative Port:** The Konflux templates default to port 5001 to avoid this conflict. Verify your `scripts/deploy-local-dev.env` contains:

```bash
REGISTRY_HOST_PORT=5001
```

The internal registry becomes accessible at localhost:5001 from your host machine.

**Disable Port Binding:** To make the registry accessible only from within the cluster, disable host port binding:

```bash
ENABLE_REGISTRY_PORT=0
```

This prevents port conflicts but requires accessing the registry through kubectl port-forwarding.

**Verify Port Availability:** Check if a port is in use before deployment:

```bash
lsof -i :5000
lsof -i :5001
```

No output indicates the port is available. If you see output, another process is using the port.

### macOS Memory Issues

Podman machine runs out of memory during builds.

Check current memory allocation:

```bash
podman machine inspect | grep Memory
```

Create a new machine with more memory:

```bash
podman machine stop
podman machine rm konflux-dev
podman machine init --memory 20480 --cpus 6 --rootful konflux-dev
podman machine start konflux-dev
```

Configure the deployment to use the new machine:

```bash
# In scripts/deploy-local-dev.env
PODMAN_MACHINE_NAME="konflux-dev"
```

See [Mac Setup Guide](mac-setup.md#memory-and-cpu-recommendations) for detailed sizing guidance.

### macOS ARM64 Issues

Deployment automatically detects ARM64 architecture and uses the appropriate Kind configuration.

Verify detection:

```bash
uname -m
# Should show: arm64
```

Verify the correct Kind config is being used:

```bash
ls -l kind-config-arm64.yaml
```

If you encounter architecture-related issues, explicitly specify the Kind config:

```bash
kind create cluster --name konflux --config kind-config-arm64.yaml
```

### Kind Cluster Creation Fails

If cluster creation hangs or fails, verify your container runtime is running.

**Podman users:**

```bash
podman machine list
```

Restart the Podman machine:

```bash
podman machine stop
podman machine start
```

**Docker users:**

Verify Docker Desktop is running and healthy.

**Clean up and retry:**

```bash
kind delete cluster --name konflux
./scripts/deploy-local-dev.sh my-konflux.yaml
```

## Cluster Restart Issues

### Cluster Needs Restart

The host machine was restarted, went to sleep, or the cluster is experiencing persistent issues.

Restart the Kind cluster container:

```bash
# Podman
podman restart konflux-control-plane

# Docker
docker restart konflux-control-plane
```

Reapply system limits after restart:

```bash
# Increase inotify limits (if host was restarted)
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512

# Increase PID limit
podman update --pids-limit 4096 konflux-control-plane
```

Wait several minutes for the UI to become available. The cluster components need time to restart.

## Docker Rate Limiting

Docker Hub enforces rate limits on image pulls. When you exceed these limits, deployments fail with "toomanyrequests" errors. You can detect this issue by checking Kubernetes events for rate limiting messages, then pre-load affected images into your Kind cluster to avoid pulling from Docker Hub.

### Detecting Rate Limit Errors

Check your cluster events for rate limiting messages:

```bash
kubectl get events -A | grep toomanyrequests
```

If this command returns any events, you're experiencing Docker Hub rate limiting.

### Pre-loading Images

Pre-load the affected image into your Kind cluster to bypass Docker Hub pulls. This example demonstrates the process with the registry image, but you can apply the same steps to any rate-limited image.

First, authenticate with Docker Hub (optional but recommended to increase your rate limit):

```bash
podman login docker.io
```

Then pull and load the image into your Kind cluster:

```bash
# Pull the image (uses authentication if configured)
podman pull registry:2

# Load it into your Kind cluster
kind load docker-image registry:2 --name konflux
```

Once the image is pre-loaded, continue with your normal deployment:

```bash
./deploy-deps.sh
```

The deployment will now use the pre-loaded image instead of pulling from Docker Hub, avoiding rate limit issues.

## Getting Help

If you encounter issues not covered in this guide:

1. Check the [operator logs](#operator-not-starting) for error messages
2. Review [component-specific logs](#components-not-deploying) for failure details
3. Verify your configuration against the [sample Konflux CRs](../operator/config/samples/)
4. Search existing [GitHub issues](https://github.com/konflux-ci/konflux-ci/issues)
5. Create a new issue with relevant logs and configuration

Include these details when reporting issues:

- Kubernetes version and platform (Kind, OpenShift, EKS, etc.)
- Operator version
- Relevant CR configuration (sanitized to remove secrets)
- Error messages from operator and component logs
- Output of `kubectl get konflux konflux -o yaml`

## Related Documentation

- [Operator Deployment Guide](operator-deployment.md) - Deployment instructions
- [Mac Setup Guide](mac-setup.md) - macOS-specific configuration
- [Demo Users Configuration](demo-users.md) - Authentication setup
- [Konflux Operator Samples](../operator/config/samples/) - Example configurations
