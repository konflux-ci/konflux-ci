Troubleshooting Docker Hub Rate Limits
===

Docker Hub enforces rate limits on image pulls. If you encounter script failures related
to rate limiting, you can use the procedure below on Kind to pre-load the image locally
and avoid those issues.

# Detection

The following command should return some content:

```bash
kubectl get events -A | grep toomanyrequests
```

# Solution

Pre-load the image into your Kind cluster to avoid pulling from Docker Hub (this example
uses the registry image):

:gear: Authenticate with Docker Hub (optional but recommended):

```bash
podman login docker.io
```

:gear: Pre-load the registry image:

```bash
# Pull the image (uses authentication if configured)
podman pull registry:2

# Load it into your Kind cluster
kind load docker-image registry:2 --name konflux
```

:gear: Continue with normal deployment:

```bash
./deploy-deps.sh
```
