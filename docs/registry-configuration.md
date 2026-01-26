# Container Registry Configuration

Konflux requires every Component to specify a `containerImage` field that defines where built container images should be stored. This guide helps you choose and configure the right registry setup for your needs.

<!-- toc -->

- [Choosing Your Registry Setup](#choosing-your-registry-setup)
  * [Comparison Table](#comparison-table)
  * [Decision Guide](#decision-guide)
- [Internal Registry (Kind/Local Development)](#internal-registry-kindlocal-development)
- [External Registries](#external-registries)
  * [Configuring a Push Secret for the Build Pipeline](#configuring-a-push-secret-for-the-build-pipeline)
  * [Configuring a Push Secret for the Release Pipeline](#configuring-a-push-secret-for-the-release-pipeline)
- [Automatic Repository Provisioning (Quay.io)](#automatic-repository-provisioning-quayio)

<!-- tocstop -->

## Choosing Your Registry Setup

Konflux supports three registry approaches, each suited to different use cases.

### Comparison Table

| Feature | Internal Registry | Quay.io | Other External Registries |
|---------|------------------|---------|---------------------------|
| **Best for** | Local development, testing | Production, auto-provisioning | Production, existing infrastructure |
| **Setup complexity** | Minimal (automatic) | Medium (requires organization & token) | Medium (requires credentials) |
| **Automatic repo creation** | No (must exist) | Yes (with image-controller) | No (must pre-create) |
| **Publicly accessible** | No | Yes | Depends on registry |
| **Persistence** | No (lost on cluster deletion) | Yes | Yes |
| **High availability** | No | Yes | Depends on registry |
| **Cost** | Free | Free tier available | Varies by provider |

### Decision Guide

**Use the internal registry when:**
- Developing and testing locally on Kind clusters
- Experimenting with Konflux features
- Building images that don't need to persist beyond your current cluster
- You want zero external configuration

**Use Quay.io with automatic provisioning when:**
- Onboarding components through the Konflux UI (required)
- You want repositories created automatically when adding components
- Building production applications that need publicly accessible images
- You prefer a fully managed registry solution

**Use other external registries when:**
- Your organization has existing registry infrastructure
- You need to comply with specific hosting requirements
- Using cloud provider registries (ECR, GCR, ACR)
- You have specific registry features or policies to maintain

**Important limitations of the internal registry:**
- Only accessible from within the cluster (not from your host or external services)
- Data is lost when the Kind cluster is deleted
- No replication, backup, or access controls
- Not suitable for production deployments

## Internal Registry (Kind/Local Development)

The local development setup automatically deploys an internal OCI registry at `registry-service.kind-registry.svc.cluster.local` within your cluster.

Components use the internal registry by specifying it in the `containerImage` field:

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: Component
metadata:
  name: test-component
  namespace: user-ns2
spec:
  componentName: test-component
  application: test-application
  source:
    git:
      url: https://github.com/your-org/your-repo.git
  containerImage: registry-service.kind-registry.svc.cluster.local/user-ns2/test-component:latest
```

The internal registry requires no configuration for local development. Your build and release pipelines can push to this registry without authentication.

**Port access:** The registry is also exposed on localhost:5001 for external access from your development machine. This allows you to pull images built in the cluster or push images for testing.

## External Registries

External registries provide persistence, public accessibility, and production-grade features. Any OCI-compliant registry works with Konflux, including Docker Hub, Quay.io, GitHub Container Registry, Google Container Registry, Amazon ECR, and Azure Container Registry.

### Configuring a Push Secret for the Build Pipeline

After the build pipeline builds an image, it pushes to the registry specified in the Component's `containerImage` field. If using a registry that requires authentication, configure a push secret in your namespace.

Tekton injects push secrets into pipelines by attaching them to a service account. The service account used for running pipelines is created by Build Service and named `build-pipeline-<component-name>`.

1. Create the secret in the pipeline's namespace:

Replace `$NS` with the correct namespace. For example:
- for user1, specify 'user-ns1'
- for user2, specify 'user-ns2'
- for managed1, specify 'managed-ns1'
- for managed2, specify 'managed-ns2'

```bash
kubectl create -n $NS secret generic regcred \
 --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
 --type=kubernetes.io/dockerconfigjson
```

**Obtaining registry credentials:**

For **Quay.io**:
1. Log into quay.io and click your user icon on the top-right corner
2. Select Account Settings
3. Click on Generate Encrypted Password
4. Enter your login password and click Verify
5. Select Docker Configuration
6. Click Download `<your-username>-auth.json` and note the download location
7. Use this path for `<path/to/.docker/config.json>` in the kubectl command

For **Docker Hub**:
1. Log in to Docker Hub
2. Go to Account Settings → Security
3. Click "New Access Token"
4. Generate a token with read/write permissions
5. Use `docker login` with your username and token to create the config.json file
6. Find the config file at `~/.docker/config.json`

For **other registries**: Follow your registry provider's documentation to obtain a Docker config.json file with authentication credentials.

2. Add the secret to the component's `build-pipeline-<component-name>` service account:

```bash
kubectl patch -n $NS serviceaccount "build-pipeline-${COMPONENT_NAME}" -p '{"secrets": [{"name": "regcred"}]}'
```

### Configuring a Push Secret for the Release Pipeline

If the release pipeline needs to push images to a container registry, configure a push secret in the managed namespace.

In the `managed` namespace, repeat the same steps mentioned [above](#configuring-a-push-secret-for-the-build-pipeline) for configuring the push secret.

## Automatic Repository Provisioning (Quay.io)

**Note**: This configuration is mandatory for onboarding components using the Konflux UI.

Konflux integrates with the [Image Controller](https://github.com/konflux-ci/image-controller) to automatically create Quay.io repositories when onboarding a component. The image controller requires access to a Quay organization.

Follow these steps to configure automatic repository provisioning:

1. [Create a user on Quay.io](https://quay.io/)

2. [Create a Quay Organization](https://docs.projectquay.io/use_quay.html#org-create)

3. [Create an Application and OAuth access token](https://docs.projectquay.io/use_quay.html#creating-oauth-access-token)

The application should have the following permissions:
   - Administer Organization
   - Administer Repositories
   - Create Repositories

4. Run the `deploy-image-controller.sh` script:

`$TOKEN` - the token for the application you created in step 3.

`$ORGANIZATION` - the name of the organization you created in step 2.

```bash
./deploy-image-controller.sh $TOKEN $ORGANIZATION
```

The script creates a secret in the `image-controller` namespace and patches the Konflux CR to enable the image controller. When you onboard a component through the UI, the image controller automatically creates a Quay repository in your organization.

**Using the image controller with Kubernetes manifests:**

When creating Components manually via YAML, you can still use automatic repository provisioning. Leave the `containerImage` field empty in your Component resource:

```yaml
apiVersion: appstudio.redhat.com/v1alpha1
kind: Component
metadata:
  name: test-component
  namespace: user-ns2
spec:
  componentName: test-component
  application: test-application
  source:
    git:
      url: https://github.com/your-org/your-repo.git
  # containerImage field is empty - image-controller will set it automatically
```

The image controller detects the empty `containerImage` field and automatically provisions a Quay repository, then updates the Component with the correct image URL.
