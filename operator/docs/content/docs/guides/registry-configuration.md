---
title: "Registry Configuration"
linkTitle: "Registry Configuration"
weight: 3
description: "Configuring container registries for build and release pipelines."
---

Konflux supports any OCI-compliant container registry for storing built images.

For **local Kind deployments**, the internal OCI registry
(`registry-service.kind-registry.svc.cluster.local`, exposed on `localhost:5001`)
works out of the box with no authentication required. Note that images stored in the
internal registry are lost when the Kind cluster is deleted.

For **production deployments**, use an external registry. The sections below cover
obtaining registry credentials, creating push secrets for build and release pipelines,
and optionally enabling the [image-controller](#quayio-auto-provisioning-image-controller)
for automatic Quay repository provisioning when onboarding components via the UI.

## Obtaining registry credentials

Configuring push secrets for build and release pipelines requires registry credentials
in the form of a Docker `config.json` file. The steps below describe how to obtain
this file for the most common registry providers.

### Quay.io

1. Log in to [quay.io](https://quay.io) and click your user icon in the top-right corner.
2. Select **Account Settings**.
3. Click **Generate Encrypted Password**.
4. Enter your login password and click **Verify**.
5. Select **Docker Configuration**.
6. Click **Download `<your-username>-auth.json`** and note the download location.
7. Use that path in the `kubectl create secret` command below.

### Docker Hub

1. Log in to [Docker Hub](https://hub.docker.com) and navigate to **Account Settings → Security**.
2. Create a new access token with read/write permissions.
3. Authenticate locally to generate a `config.json`:

```bash
podman login docker.io
```

4. The config file is written to `${XDG_RUNTIME_DIR}/containers/auth.json` (Podman)
   or `~/.docker/config.json` (Docker). Use that path in the `kubectl create secret`
   command below.

### Other registries

Follow your registry provider's documentation to obtain a Docker `config.json` with
authentication credentials. Most registries support `podman login <registry>` or
`docker login <registry>` to generate the file.

## Build pipeline push secret

After the build pipeline builds an image it pushes it to a container registry.
If using a registry that requires authentication, the namespace where the pipeline
runs must be configured with a push secret.

Tekton injects push secrets into pipelines by attaching them to a service account.
The service account used for running the pipelines is created by the Build Service
Operator and is named `build-pipeline-<component-name>`.

1. Create the secret in the pipeline namespace (replace `$NS` with your namespace,
   e.g. `user-ns1`):

```bash
kubectl create -n $NS secret generic regcred \
  --from-file=.dockerconfigjson=<path/to/config.json> \
  --type=kubernetes.io/dockerconfigjson
```

2. Attach the secret to the component's build pipeline service account:

```bash
kubectl patch -n $NS serviceaccount "build-pipeline-${COMPONENT_NAME}" \
  -p '{"secrets": [{"name": "regcred"}]}'
```

## Release pipeline push secret

If the release pipeline pushes images to a container registry it needs its own push
secret. The release pipeline runs under the service account named in the
`ReleasePlanAdmission` (e.g. `release-pipeline` in the demo resources).

1. In the **managed** namespace (e.g. `managed-ns2`), create the secret the same way
   as for the build pipeline:

```bash
kubectl create -n $NS secret generic regcred \
  --from-file=.dockerconfigjson=<path/to/config.json> \
  --type=kubernetes.io/dockerconfigjson
```

2. Attach it to the release pipeline service account:

```bash
kubectl patch -n $NS serviceaccount release-pipeline \
  -p '{"secrets": [{"name": "regcred"}]}'
```

### Trusted Artifacts (ociStorage)

If the release pipeline uploads [Trusted Artifacts](https://konflux-ci.dev/docs/building/using-trusted-artifacts/), set the `ociStorage` field in your `ReleasePlanAdmission` to your own OCI storage URL (e.g. your registry path). Ensure the `release-pipeline` service account has credentials to push to that location (e.g. an additional registry secret or Quay token linked to that service account).


```yaml
# In your ReleasePlanAdmission
spec:
  pipeline:
    pipelineRef:
      ociStorage: quay.io/my-org/my-component-release-ta
```

For local Kind deployments using the internal registry:

```yaml
# In your ReleasePlanAdmission
spec:
  pipeline:
    pipelineRef:
      ociStorage: registry-service.kind-registry/test-component-release-ta
```

## Quay.io auto-provisioning (image-controller)

The [image-controller](https://github.com/konflux-ci/image-controller) automatically
creates Quay repositories when a component is onboarded via the Konflux UI. This is
required for the UI-based onboarding flow.
It is optional when creating components directly with Kubernetes manifests.

### Step 1: Create a Quay organization and OAuth token

1. [Create a Quay.io account](https://quay.io) if you don't have one.
2. [Create a Quay organization](https://docs.projectquay.io/quay_io.html#org-create).
3. [Create an OAuth Application and generate an access token](https://docs.projectquay.io/api_quay.html#creating-oauth-access-token):
   - In your Quay organization, go to **Applications** → **Create New Application**
   - Click on the application name → **Generate Token**
   - Select these permissions:
     - Administer Organization
     - Administer Repositories
     - Create Repositories

4. Click **Generate Access Token → Authorize Application** and copy the token. This
   is your only opportunity to view it.

### Step 2: Enable image-controller and create the secret

**Option A — local Kind deployment via `deploy-local.sh` (recommended):**

Set the environment variables before running the script. This automatically selects
the `konflux-e2e.yaml` sample CR which has `imageController.enabled: true`:

```bash
export QUAY_TOKEN="<token from step 3>"
export QUAY_ORGANIZATION="<organization from step 2>"
./scripts/deploy-local.sh
```

**Option B — manual setup on an existing cluster:**

First, enable image-controller in your Konflux CR:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  imageController:
    enabled: true
```

Then create the secret in the `image-controller` namespace:

```bash
kubectl -n image-controller create secret generic quaytoken \
  --from-literal=quaytoken="<token from step 3>" \
  --from-literal=organization="<organization from step 2>"
```

## Self-hosted Quay registry

The image-controller can be configured to work with a self-hosted Quay instance
instead of the public quay.io. This requires two things: pointing image-controller
to the self-hosted Quay API and, if the instance uses a custom CA certificate,
providing that certificate.

### Step 1: Configure the Quay API URL

To point image-controller at a self-hosted Quay instance, add the `quayapiurl`
key to the `quaytoken` secret in the `image-controller` namespace. The value
should be the full API URL including the `/api/v1` suffix.

If you are creating the secret for the first time:

```bash
kubectl -n image-controller create secret generic quaytoken \
  --from-literal=quaytoken="<OAuth token>" \
  --from-literal=organization="<organization>" \
  --from-literal=quayapiurl="https://quay.example.com/api/v1"
```

If the secret already exists, patch it:

```bash
kubectl -n image-controller patch secret quaytoken \
  -p '{"stringData":{"quayapiurl":"https://quay.example.com/api/v1"}}'
```

When `quayapiurl` is not set, image-controller defaults to the public Quay API
(`https://quay.io/api/v1`).

### Step 2: Provide a custom CA certificate (optional)

If the self-hosted Quay instance uses a TLS certificate signed by a custom CA
(i.e. not trusted by the system CA bundle), you must provide the CA certificate
so that image-controller can verify the connection.

1. Create a ConfigMap in the `image-controller` namespace with the CA certificate:

```bash
kubectl -n image-controller create configmap quay-ca-bundle \
  --from-file=ca-bundle.crt=/path/to/your/ca-certificate.crt
```

2. Configure the CA bundle in your Konflux CR:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  imageController:
    enabled: true
    spec:
      quayCABundle:
        configMapName: quay-ca-bundle
        key: ca-bundle.crt
```

The operator will mount the CA certificate into the image-controller pod and set
the `QUAY_ADDITIONAL_CA` environment variable pointing to it. The certificate is
appended to the system CA pool, so connections to other registries are not affected.

The `configMapName` and `key` fields allow you to use any ConfigMap name and key.
For example, if your ConfigMap has the certificate under a different key:

```bash
kubectl -n image-controller create configmap my-ca \
  --from-file=quay-ca.crt=/path/to/ca.crt
```

```yaml
spec:
  imageController:
    enabled: true
    spec:
      quayCABundle:
        configMapName: my-ca
        key: quay-ca.crt
```
