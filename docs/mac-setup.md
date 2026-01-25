# Konflux Setup Guide for macOS

This guide covers macOS-specific configuration for running Konflux locally using Kind and Podman.

## Prerequisites

Install required tools using Homebrew:

```bash
brew install kind kubectl podman
```

## Podman on macOS

Podman provides a lightweight, daemonless alternative to Docker. On macOS, Podman runs containers inside a Linux virtual machine.

### Installation

Install Podman from Homebrew:

```bash
brew install podman
```

Initialize the default Podman machine:

```bash
podman machine init
podman machine start
```

Verify the installation:

```bash
podman version
podman machine list
podman run hello-world
```

## Podman Machine Configuration

Konflux requires a Podman machine with sufficient resources to run the Kind cluster and Konflux components.

### Creating a Dedicated Machine

Stop the default machine and create a dedicated machine with appropriate resources:

```bash
podman machine stop
podman machine init \
  --memory 16384 \
  --cpus 6 \
  --disk-size 100 \
  --rootful \
  konflux-dev
podman machine start konflux-dev
podman system connection default konflux-dev
```

The `--rootful` flag creates a machine running as root, which Kind requires for proper cluster operation. Memory is set to 16GB (16384 MB), CPUs to 6 cores, and disk size to 100GB.

### Resource Requirements

Minimum configuration requires 12GB memory (12288 MB), 4 CPU cores, and 50GB disk space. This supports basic Konflux operation with limited concurrent builds.

Recommended configuration uses 16GB memory (16384 MB), 6 CPU cores, and 100GB disk space. This provides comfortable headroom for multiple concurrent builds and smoother UI operation.

The Kind cluster itself needs approximately 8GB for Konflux components. The Podman VM adds 4GB overhead. Build pipelines require additional memory and CPU during execution.

### Using a Specific Machine

If you maintain multiple Podman machines, specify which one the deployment should use. Set `PODMAN_MACHINE_NAME` in `scripts/deploy-local-dev.env`:

```bash
PODMAN_MACHINE_NAME="konflux-dev"
```

The deployment script switches to this machine automatically and restores your default connection afterward.

Check your current machine configuration:

```bash
podman machine inspect konflux-dev | grep -E '"Memory"|"CPUs"'
podman machine list
```

## Port Conflicts

For port conflicts on macOS, see [macOS Port Conflicts](troubleshooting.md#macos-port-conflicts).

## Architecture Support

Konflux on macOS requires Apple Silicon (ARM64) Macs. The deployment scripts automatically detect ARM64 architecture and use `kind-config-arm64.yaml`. The ARM64 configuration includes architecture-specific container runtime settings and NodePort mappings optimized for Apple Silicon.

## Memory and CPU Recommendations

Resource allocation depends on your development workload and available system resources.

### Light Development

For UI testing and building a single component, allocate 8GB to the Kind cluster and 12GB to the Podman machine with 4 CPU cores. Set `KIND_MEMORY_GB=8` in `scripts/deploy-local-dev.env`.

### Medium Development

For multiple components with occasional builds, allocate 12GB to the Kind cluster and 16GB to the Podman machine with 6 CPU cores. Set `KIND_MEMORY_GB=12`.

### Heavy Development

For full stack development with frequent builds, allocate 16GB to the Kind cluster and 20GB to the Podman machine with 8 CPU cores. Set `KIND_MEMORY_GB=16`.

Configure memory allocation in `scripts/deploy-local-dev.env`:

```bash
KIND_MEMORY_GB=12
```

Ensure your Podman machine has sufficient memory to accommodate the Kind cluster plus overhead. The Podman machine needs KIND_MEMORY_GB plus 4GB for VM overhead.

Initialize a Podman machine with 16GB memory:

```bash
podman machine init --memory 16384 --cpus 6 --rootful konflux-dev
```

### Monitoring Resources

Monitor resource usage to identify bottlenecks:

```bash
# Inside Podman VM
podman machine ssh -- top

# Kubernetes node resources
kubectl top nodes
kubectl top pods -A
```

## Common Issues

### Podman Machine Won't Start

If `podman machine start` fails, the machine may be corrupted. Remove and recreate it:

```bash
podman machine rm konflux-dev
podman machine init --memory 16384 --cpus 6 --disk-size 100 --rootful konflux-dev
podman machine start konflux-dev
```

### Insufficient Memory

The deployment script checks for sufficient Podman machine memory. If you see "ERROR: Insufficient Podman machine memory," check the current allocation:

```bash
podman machine inspect | grep Memory
```

Create a new machine with more memory:

```bash
podman machine init --memory 20480 --cpus 6 --rootful konflux-large
podman machine start konflux-large
```

Configure the deployment to use this machine by setting `PODMAN_MACHINE_NAME="konflux-large"` in `scripts/deploy-local-dev.env`.

### Kind Cluster Creation Fails

See [Kind Cluster Creation Fails](troubleshooting.md#kind-cluster-creation-fails) for detailed troubleshooting steps.

### PID Limit Exhausted

Tekton pipelines may fail with "cannot fork" errors if the PID limit is too low. The deployment script increases the limit automatically, but if you still see errors, increase it manually:

```bash
podman update --pids-limit 8192 konflux-control-plane
```

To disable automatic PID limit adjustment and set it manually, use `INCREASE_PODMAN_PIDS_LIMIT=0` in `scripts/deploy-local-dev.env`.

### Slow Performance

Builds run slowly or the UI feels sluggish when the system lacks resources. First, increase Podman machine resources:

```bash
podman machine init --memory 20480 --cpus 8 --rootful konflux-large
```

Second, reduce replica counts in `my-konflux.yaml` to consume fewer resources:

```yaml
ui:
  spec:
    proxy:
      replicas: 1
```

Third, close other applications to free system memory.

Monitor activity to identify resource bottlenecks:

```bash
kubectl top pods -A
```

## Best Practices

### Resource Management

Use a dedicated Podman machine for Konflux development. Set `PODMAN_MACHINE_NAME` if you maintain multiple machines. Monitor memory usage with `kubectl top nodes`. Clean up old clusters with `kind delete cluster --name old-cluster`.

### Port Management

Use port 5001 (not 5000) for the registry to avoid macOS conflicts. Document custom ports if you use non-standard configurations. Check port availability before deployment with `lsof -i :5001`.

### Multiple Environments

Create separate Podman machines for different projects. For Konflux development, use `podman machine init --memory 16384 konflux-dev`. For other projects, use `podman machine init --memory 8192 other-project`.

Switch between machines as needed:

```bash
podman machine start konflux-dev
podman system connection default konflux-dev
```

### Cleanup

Remove the Kind cluster when not in use:

```bash
kind delete cluster --name konflux
```

Stop the Podman machine to free system resources:

```bash
podman machine stop konflux-dev
```

Remove the Podman machine for complete cleanup:

```bash
podman machine rm konflux-dev
```

## Related Documentation

- [Operator Deployment Guide](operator-deployment.md) - Complete deployment instructions
- [Troubleshooting Guide](troubleshooting.md) - Common issues across all platforms
- [Podman Documentation](https://podman.io/docs) - Official Podman reference
