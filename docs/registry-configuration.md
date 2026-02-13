Container Registry Configuration
===

Konflux supports any OCI-compliant container registry for storing built images.

For local development on Kind, the internal registry
(`registry-service.kind-registry.svc.cluster.local`, exposed on `localhost:5001`)
works out of the box with no authentication required. Note that images stored in the
internal registry are lost when the Kind cluster is deleted.

For production deployments, use an external registry which can be accomplished by
manually creating and linking the secrets as described below. To fully onboard
components through the Konflux UI, you must configure the
[image-controller](#automatically-provision-quay-repositories-for-container-images)
which automatically provisions Quay.io repositories when components are created.

<!-- toc -->

- [Configuring a Push Secret for the Build Pipeline](#configuring-a-push-secret-for-the-build-pipeline)
  * [Obtaining Registry Credentials](#obtaining-registry-credentials)
- [Configuring a Push Secret for the Release Pipeline](#configuring-a-push-secret-for-the-release-pipeline)
  * [Trusted Artifacts (ociStorage)](#trusted-artifacts-ocistorage)
- [Automatically Provision Quay Repositories for Container Images](#automatically-provision-quay-repositories-for-container-images)

<!-- tocstop -->

# Configuring a Push Secret for the Build Pipeline

After the build-pipeline builds an image, it will try to push it to a container registry.
If using a registry that requires authentication, the namespace where the pipeline is
running should be configured with a push secret for the registry.

Tekton provides a way to inject push secrets into pipelines by attaching them to a
service account.

The service account used for running the pipelines is created by Build Service operator
and named `build-pipeline-<component-name>`.

1. :gear: Create the secret in the pipeline's namespace (see
   [obtaining registry credentials](#obtaining-registry-credentials) for creating
   the config.json file):

Replace $NS with the correct namespace. For example:
- for user1, specify 'user-ns1'
- for user2, specify 'user-ns2'

```bash
kubectl create -n $NS secret generic regcred \
 --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
 --type=kubernetes.io/dockerconfigjson
```

2. :gear: Add the secret to the component's `build-pipeline-<component-name>` service account:

```bash
kubectl patch -n $NS serviceaccount "build-pipeline-${COMPONENT_NAME}" -p '{"secrets": [{"name": "regcred"}]}'
```

## Obtaining Registry Credentials

The push secret requires a Docker config.json file with credentials for your registry.

**Quay.io:**

1. :gear: Log into quay.io and click your user icon on the top-right corner.

2. :gear: Select Account Settings.

3. :gear: Click on Generate Encrypted Password.

4. :gear: Enter your login password and click Verify.

5. :gear: Select Docker Configuration.

6. :gear: Click Download `<your-username>-auth.json` and take note of the download
   location.

7. :gear: Replace `<path/to/.docker/config.json>` on the `kubectl create secret` command
   with this path.

**Docker Hub:**

1. :gear: Log in to Docker Hub and navigate to Account Settings > Security.

2. :gear: Create a new access token with read/write permissions.

3. :gear: Authenticate locally to generate a config.json:

```bash
podman login docker.io
```

4. :gear: The config file will be at `${XDG_RUNTIME_DIR}/containers/auth.json` (Podman)
   or `~/.docker/config.json` (Docker). Use this path in the `kubectl create secret`
   command.

**Other registries:** Follow your registry provider's documentation to obtain a Docker
config.json file with authentication credentials. Most registries support
`podman login` or `docker login` to generate the file.

# Configuring a Push Secret for the Release Pipeline

If the release pipeline needs to push images to a container registry, it needs to be
configured with a push secret as well. The release pipeline runs as the service account
named in the ReleasePlanAdmission (e.g. **release-pipeline** in the demo resources).

1. :gear: In the **managed** namespace, create the secret as in step 1
   [above](#configuring-a-push-secret-for-the-build-pipeline). Replace $NS with the
   managed namespace (e.g. `managed-ns1`, `managed-ns2`).

2. :gear: Add the secret to the release pipeline service account:

```bash
kubectl patch -n $NS serviceaccount release-pipeline -p '{"secrets": [{"name": "regcred"}]}'
```

## Trusted Artifacts (ociStorage)

If the release pipeline uploads Trusted Artifacts, set the **ociStorage** field in your
ReleasePlanAdmission to your own OCI storage URL (e.g. your registry path). Ensure the
**release-pipeline** service account has credentials to push to that location (e.g. an
additional registry secret or Quay token linked to that service account).

# Automatically Provision Quay Repositories for Container Images

**Note**: This step is mandatory for importing components using the UI.

Konflux integrates with the
[Image Controller](https://github.com/konflux-ci/image-controller)
that can automatically create Quay repositories when onboarding a component.
The image controller requires access to a Quay organization.
Please follow the following steps for configuring it:

1. :gear: [Create a user on Quay.io](https://quay.io/)

2. :gear: [Create Quay Organization](https://docs.projectquay.io/use_quay.html#org-create)

3. :gear: [Create Application and OAuth access token](https://docs.projectquay.io/use_quay.html#creating-oauth-access-token).
   The application should have the following permissions:
   - Administer Organization
   - Administer Repositories
   - Create Repositories

4. :gear: Enable image-controller in your Konflux CR and create the Quay token secret.

**Option A: Using deploy-local.sh (recommended for local development)**

Set these environment variables before running `deploy-local.sh`:

```bash
export QUAY_TOKEN="<token from step 3>"
export QUAY_ORGANIZATION="<organization from step 2>"
./scripts/deploy-local.sh
```

**Option B: Manual setup (for existing clusters)**

First, ensure image-controller is enabled in your Konflux CR:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  imageController:
    enabled: true
```

Then create the secret:

```bash
kubectl -n image-controller create secret generic quaytoken \
    --from-literal=quaytoken="<token from step 3>" \
    --from-literal=organization="<organization from step 2>"
```
