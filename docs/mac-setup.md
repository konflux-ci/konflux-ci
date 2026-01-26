# Konflux Setup Guide for macOS

This guide covers macOS-specific setup for running Konflux locally using Kind and Podman.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Podman Installation](#podman-installation)
- [Podman Machine Configuration](#podman-machine-configuration)
- [Port 5000 Conflict Resolution](#port-5000-conflict-resolution)
- [Architecture Support](#architecture-support)
- [Memory and CPU Recommendations](#memory-and-cpu-recommendations)
- [Common Issues](#common-issues)

## Prerequisites

Install required tools using Homebrew:

```bash
brew install kind kubectl podman
```

## Podman Installation

Podman replaces Docker on macOS for running Konflux. It provides a lightweight, daemonless container runtime.

### Basic Setup

```bash
# Install Podman
brew install podman

# Initialize default Podman machine
podman machine init

# Start the machine
podman machine start
```

### Verify Installation

```bash
# Check Podman version
podman version

# List Podman machines
podman machine list

# Test Podman
podman run hello-world
```

## Podman Machine Configuration

For Konflux, you need a Podman machine with sufficient resources.

### Create Dedicated Podman Machine

```bash
# Stop default machine if running
podman machine stop

# Create new machine with proper resources
podman machine init \
  --memory 16384 \
  --cpus 6 \
  --disk-size 100 \
  --rootful \
  konflux-dev

# Start the machine
podman machine start konflux-dev

# Set as default
podman system connection default konflux-dev
```

### Resource Sizing

**Minimum Configuration:**
- Memory: 12GB (12288 MB)
- CPUs: 4
- Disk: 50GB

**Recommended Configuration:**
- Memory: 16GB (16384 MB)
- CPUs: 6
- Disk: 100GB

**Why these resources?**
- Kind cluster needs ~8GB for Konflux components
- Podman VM overhead: ~4GB
- Build pipelines: Additional memory/CPU during builds

### Using Specific Podman Machine

If you have multiple Podman machines, specify which one to use:

```bash
# In scripts/deploy-local-dev.env
PODMAN_MACHINE_NAME="konflux-dev"
```

The setup script will automatically switch to this machine and restore your default afterward.

### Check Current Machine Resources

```bash
# View machine configuration
podman machine inspect konflux-dev | grep -E '"Memory"|"CPUs"'

# View running machine
podman machine list
```

## Port 5000 Conflict Resolution

macOS uses port 5000 for the AirPlay Receiver service, which conflicts with the default registry port.

### Option 1: Disable AirPlay Receiver (Recommended)

1. Open System Settings
2. Navigate to: **General → AirDrop & Handoff**
3. Turn off: **AirPlay Receiver**

### Option 2: Use Different Port

```bash
# In scripts/deploy-local-dev.env
REGISTRY_HOST_PORT=5001
```

The default template already uses 5001 to avoid this conflict.

### Option 3: Disable Port Binding

```bash
# In scripts/deploy-local-dev.env
ENABLE_REGISTRY_PORT=0
```

Registry will only be accessible from within the cluster (not from host).

### Verify Port Availability

```bash
# Check if port is in use
lsof -i :5000
lsof -i :5001

# If port is free, no output is shown
```

## Architecture Support

Konflux supports both Intel (x86_64) and Apple Silicon (ARM64) Macs.

### Apple Silicon (M1/M2/M3)

The setup scripts automatically detect ARM64 and use the appropriate Kind configuration.

**Automatic Detection:**
- Uses `kind-config-arm64.yaml` on ARM64 systems
- No manual configuration needed

**Verification:**

```bash
# Check architecture
uname -m
# Output: arm64 (Apple Silicon) or x86_64 (Intel)

# Verify Kind config
cat kind-config-arm64.yaml
```

### Intel Macs

Intel Macs use the standard `kind-config.yaml` automatically.

### Cross-Platform Builds

For building images that work on both architectures:

```bash
# Build multi-arch image
podman build --platform=linux/amd64,linux/arm64 -t myimage .

# Or specify single platform
podman build --platform=linux/arm64 -t myimage .
```

## Memory and CPU Recommendations

### Development Workload

**Light Usage** (UI testing, single component):
- KIND_MEMORY_GB: 8
- Podman Memory: 12GB
- Podman CPUs: 4

**Medium Usage** (multiple components, occasional builds):
- KIND_MEMORY_GB: 12
- Podman Memory: 16GB
- Podman CPUs: 6

**Heavy Usage** (full stack, frequent builds):
- KIND_MEMORY_GB: 16
- Podman Memory: 20GB
- Podman CPUs: 8

### Configure Memory

```bash
# In scripts/deploy-local-dev.env
KIND_MEMORY_GB=12

# Ensure Podman machine has enough (KIND_MEMORY_GB + 4GB overhead)
podman machine init --memory 16384 ...
```

### Monitor Resource Usage

```bash
# View Podman machine resource usage
podman machine ssh -- top

# View Kind node resources
kubectl top nodes
kubectl top pods -A
```

## Common Issues

### Podman Machine Won't Start

**Symptom:** `podman machine start` fails

**Solution:**

```bash
# Remove corrupted machine
podman machine rm konflux-dev

# Recreate
podman machine init --memory 16384 --cpus 6 --disk-size 100 --rootful konflux-dev
podman machine start konflux-dev
```

### Insufficient Memory Error

**Symptom:** "ERROR: Insufficient Podman machine memory"

**Solution:**

```bash
# Check current memory
podman machine inspect | grep Memory

# Create new machine with more memory
podman machine init --memory 20480 --cpus 6 --rootful konflux-large
podman machine start konflux-large

# Use it in deployment
# In scripts/deploy-local-dev.env:
PODMAN_MACHINE_NAME="konflux-large"
```

### Kind Cluster Creation Fails

**Symptom:** "kind create cluster" hangs or fails

**Solution:**

```bash
# Verify Podman is running
podman machine list

# Restart Podman machine
podman machine stop
podman machine start

# Clean up and retry
kind delete cluster --name konflux
./scripts/deploy-local-dev.sh
```

### Registry Port Already in Use

**Symptom:** "ERROR: Port 5001 is already in use"

**Solution:**

```bash
# Find what's using the port
lsof -i :5001

# Stop the service or use different port
# In scripts/deploy-local-dev.env:
REGISTRY_HOST_PORT=5002
```

### PID Limit Exhausted

**Symptom:** Tekton pipelines fail with "cannot fork" errors

**Solution:**

The setup script automatically increases PID limits. If still seeing errors:

```bash
# Manually increase PID limit
podman update --pids-limit 8192 konflux-control-plane

# Or disable auto-increase and set manually
# In scripts/deploy-local-dev.env:
INCREASE_PODMAN_PIDS_LIMIT=0
```

### Slow Performance

**Symptom:** Builds are slow, UI is sluggish

**Solutions:**

1. **Increase Podman resources:**
   ```bash
   podman machine init --memory 20480 --cpus 8 ...
   ```

2. **Reduce replica counts:**
   ```yaml
   # In my-konflux.yaml
   ui:
     spec:
       proxy:
         replicas: 1  # Instead of 2-3
   ```

3. **Close other applications** to free memory

4. **Monitor activity:**
   ```bash
   kubectl top pods -A
   ```

## Best Practices

### Resource Management

- Use dedicated Podman machine for Konflux
- Set `PODMAN_MACHINE_NAME` if you have multiple machines
- Monitor memory usage: `kubectl top nodes`
- Clean up old clusters: `kind delete cluster --name old-cluster`

### Port Management

- Use port 5001 (not 5000) for registry
- Document custom ports in your team wiki
- Check port availability before deployment

### Multiple Environments

Create separate Podman machines for different projects:

```bash
# Konflux development
podman machine init --memory 16384 konflux-dev

# Other projects
podman machine init --memory 8192 other-project

# Switch between them
podman machine start konflux-dev
podman system connection default konflux-dev
```

### Cleanup

```bash
# Remove Kind cluster
kind delete cluster --name konflux

# Stop Podman machine (frees system resources)
podman machine stop konflux-dev

# Remove Podman machine (full cleanup)
podman machine rm konflux-dev
```

## Related Documentation

- [Operator Deployment Guide](operator-deployment.md) - Full deployment instructions
- [Konflux Documentation](https://konflux-ci.dev/docs/) - Official docs
- [Podman Documentation](https://podman.io/docs) - Podman reference
