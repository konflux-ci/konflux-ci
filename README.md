# konflux-ci
Integration and release of Konflux-CI


## Trying Out Konflux

The recommended way to try out Konflux is using [Kind](https://kind.sigs.k8s.io/)
Create you Kind cluster using the provided config in this repository.
The config tells Kind to forward ports from the host to the Kind cluster. Those ports
are needed for accessing Konflux.

From the root of this repository, run the following commands:

1. Install [Kind and kubectl](https://kind.sigs.k8s.io/docs/user/quick-start/#installation)

2. Create a cluster

```bash
kind create cluster --name konflux --config kind-config.yaml
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

## Required Secrets (TBA)

- has requires secret for creating gitops repos
- chains signing and push secret
    - install cosign - https://github.com/sigstore/cosign
    - connect to the kind cluster and run
    ```bash
    cosign generate-key-pair k8s://tekton-pipelines/signing-secrets

    k create secret generic public-key --from-file cosign.pub -n tekton-pipelines
    ```
- build-service github app (global or namespace)
- integration-service github app

## Accessing The UI

Add the following entry to `/etc/hosts`

```bash
127.0.0.1 ui.konflux.dev
```

Open your browser and navigate to: https://ui.konflux.dev:6443/application-pipeline

## Running A Build Pipeline

1. Configure a push secret for the component [configuring-docker-authentication-for-docker](https://tekton.dev/docs/pipelines/auth/#configuring-docker-authentication-for-docker)
