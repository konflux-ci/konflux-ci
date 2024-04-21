# Konflux-CI
Integration and release of Konflux-CI

## Trying Out Konflux

The recommended way to try out Konflux is using [Kind](https://kind.sigs.k8s.io/)
Create a Kind cluster using the provided config in this repository.
The config tells Kind to forward port `9443` from the host to the Kind cluster. The port forwarding is needed for accessing Konflux.

### Machine requirements

The deployment requires 8GB of free RAM.

### Installing the dependencies

Install [Kind and kubectl](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

Install `openssl`

### Bootstrapping the cluster

From the root of this repository, run the following commands:

1. Create a cluster

```bash
kind create cluster --name konflux --config kind-config.yaml
```

**Note:** If the cluster fails to start because of [too many open files](https://kind.sigs.k8s.io/docs/user/known-issues/#pod-errors-due-to-too-many-open-files)
run the following commands:

```bash
sudo sysctl fs.inotify.max_user_watches=524288
sudo sysctl fs.inotify.max_user_instances=512
```

3. Deploy the dependencies

```bash
./deploy-deps.sh
```

4. Deploy Konflux

```bash
./deploy-konflux.sh
```

5. Deploy demo users

```bash
./deploy-test-resources.sh
```

6. The UI will be available at https://localhost:9443. You can login using the test user.

`username:` `user1`

`password:` `password`

## Configuring Secrets

### Github Application

- build-service github app (global or namespace) - TBA
- integration-service github app - TBA

### Configuring a push secret for the build pipeline

After the build-pipeline builds an image, it will try to
push it to a container registry. The namespace where the pipeline is running should be configured with a push secret for the container registry.

Tekton provides a way to inject push secrets into pipelines by attaching them to a service account.

The service account used for running the pipelines is the `default` service account.

1. Create the secret in the namespace:

```bash
kubectl create secret generic regcred \
 --from-file=.dockerconfigjson=<path/to/.docker/config.json> \
 --type=kubernetes.io/dockerconfigjson
```

2. Add the secret to the default service account

```bash
kubectl patch serviceaccount default -p '{"secrets": [{"name": "regcred"}]}'
```

### Configuring a push secret for the release pipeline

If the release pipeline used need to push image to a container
registry, it needs to be configured with a push secret as well.

In the `managed` namespace, repeat the same steps mentioned [above](#configuring-a-push-secret-for-the-build-pipeline) for
configuring the push secret.

## Using Konflux

### Create an Application and Component

TBA

### Create Integration test for your application

TBA

### Configure a release pipeline

TBA

## Namespace and user management

### Creating a new namespace

```bash
# Replace $NS with the name of the new namespace

kubectl create namespace $NS
kubectl label namespace "$NS konflux.ci/type=user
```

Example:

```bash
kubectl create namespace user-ns3
kubectl label namespace user-ns3 konflux.ci/type=user
```

### Granting a user access to a namespace

```bash
# Replace $RB with the name of the role binding (you can choose the name)
# Replace $USER with the email address of the user
# Replace $NS with the name of the namespace the user should access

kubectl create rolebinding $RB --clusterrole konflux-admin-user-actions --user $USER -n $NS
```

Example:

```bash
kubectl create rolebinding user1-konflux --clusterrole konflux-admin-user-actions --user user1@konflux.dev -n user-ns3
```

### Add a new user

Konflux is using [Keycloak](https://www.keycloak.org/) for managing users and authentication.
The administration console for Keycloak is exposed at https://localhost:9443/idp/admin/master/console/#/redhat-external

For getting the username and password for the console run:

```bash
# USERNAME

kubectl get -n keycloak secrets/keycloak-initial-admin --template={{.data.username}} | base64 -d

# PASSWORD

kubectl get -n keycloak secrets/keycloak-initial-admin --template={{.data.password}} | base64 -d
```

After login into the console, click on the `Users` tab
on the left for adding a user.

In addition, you can configure additional `Identity providers` such as `Github`, `Google`, etc.. by clicking on the `Identity providers` tab on the left.
