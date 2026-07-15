---
title: "Webhook URL Configuration"
linkTitle: "Webhook URL Configuration"
weight: 9
description: "How to configure custom webhook URLs for git providers when using Smee proxies or self-hosted instances."
---

When Konflux sets up a repository for CI/CD, the build-service configures a
webhook on the git provider (GitHub, GitLab, etc.) so that push events trigger
pipeline runs. The webhook URL must be externally reachable by the git provider.

## When you need this

You need to configure `webhookURLs` when the default webhook URL discovery does
not match your environment:

- **Smee proxy** — You use a Smee channel or similar webhook relay to forward
  events from a public git provider to a cluster that is not directly reachable.
- **Multiple git providers** — Different providers need different webhook
  endpoints (e.g., GitHub through one proxy, GitLab through another).
- **Self-hosted git** — Your self-hosted GitHub Enterprise or GitLab instance
  requires a specific webhook endpoint.

### When you do NOT need this

- **GitHub App authentication** — When using a GitHub App, the webhook endpoint
  is defined in the app configuration itself. The build-service does not
  configure webhooks for repositories that use a GitHub App, so this feature
  is not needed.
- **OpenShift with default Pipelines-as-Code** — On OpenShift, a Route is
  automatically created for Pipelines-as-Code. The build-service discovers this
  Route and uses it as the webhook URL. No configuration is needed.

## Configuration

Add the `webhookURLs` field to the build-service section of the Konflux CR.
Keys are **repository URL prefixes** and values are the **externally-reachable
webhook URL** to configure on matching repositories.

### Single default for all providers

Use an empty string key (`""`) as a catch-all default:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      webhookURLs:
        "": "https://smee.example.com/my-channel"
```

### Per-provider configuration

Specify different webhook URLs per git provider:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  buildService:
    spec:
      webhookURLs:
        "https://github.com": "https://smee.example.com/github-channel"
        "https://gitlab.com": "https://smee.example.com/gitlab-channel"
```

### Mixed (per-provider with a default fallback)

```yaml
spec:
  buildService:
    spec:
      webhookURLs:
        "https://gitlab.cee.example.com": "https://smee.example.com/internal-gitlab"
        "": "https://smee.example.com/default-channel"
```

## How matching works

The build-service uses **longest-prefix matching** against the full repository
URL. For a repository at `https://gitlab.cee.example.com/team/my-app`, the
entries are evaluated as follows:

1. `"https://gitlab.cee.example.com"` matches (26 characters) — **selected**
2. `""` matches (0 characters) — lower priority

If no prefix matches and no default (`""`) is configured, the build-service
falls back to its built-in behavior (Route discovery on OpenShift, or the
`PAC_WEBHOOK_URL` environment variable).

## Platform behavior

The build-service uses two environment variables for PaC communication:

- **`PAC_WEBHOOK_URL`** — A fallback webhook URL used when no entry in the
  webhook config JSON matches the repository. The operator only sets it when
  no `webhookURLs` are configured — otherwise unmatched repositories would get
  an internal cluster URL registered as their webhook endpoint.
- **`PAC_NAMESPACE`** — Tells build-service where to find the PaC controller
  Service for **in-cluster retrigger** operations (the `/incoming` endpoint).
  The retrigger path goes directly to the PaC controller inside the cluster
  rather than through the external webhook chain.

| Platform | `webhookURLs` configured? | `PAC_WEBHOOK_URL` | `PAC_NAMESPACE` |
|----------|--------------------------|-------------------|-----------------|
| OpenShift | No | Not set — build-service auto-discovers the PaC Route | Not set — defaults to `openshift-pipelines` |
| OpenShift | Yes | Not set — webhook config JSON controls webhook URLs | Not set — defaults to `openshift-pipelines` |
| Non-OpenShift | No | Set to `http://pipelines-as-code-controller.tekton-pipelines.svc.cluster.local:8080` | Set to `tekton-pipelines` |
| Non-OpenShift | Yes | **Not set** — unmatched repos would get an internal URL | Set to `tekton-pipelines` |

When `webhookURLs` are configured, `PAC_WEBHOOK_URL` is left unset so that
repositories not matching any prefix fall through to Route discovery (OpenShift)
rather than getting an internal service URL registered as their webhook.
`PAC_NAMESPACE` is always set on non-OpenShift regardless of `webhookURLs`,
because retrigger always uses in-cluster communication.

User-specified values via the CR always take precedence over operator defaults.

## Advanced: overriding PAC_WEBHOOK_URL or PAC_NAMESPACE

If you need to explicitly set these variables (e.g., PaC is deployed in a
non-standard namespace), you can override them through the generic environment
variable mechanism:

```yaml
spec:
  buildService:
    spec:
      buildControllerManager:
        manager:
          env:
            - name: PAC_WEBHOOK_URL
              value: "http://custom-pac-endpoint:8080"
            - name: PAC_NAMESPACE
              value: "my-custom-pac-namespace"
```

Note that `PAC_WEBHOOK_URL` acts as a fallback — it is used for any repository
that does not match a prefix in the webhook config JSON.

## Disabling TLS verification for webhooks

The webhook endpoint may use a self-signed certificate, which is common in test
or development environments. When the build-service configures webhooks on the
git provider, TLS verification will fail against such certificates.

To skip TLS verification, set `pacWebhookInsecureSSL` to `true`:

```yaml
spec:
  buildService:
    spec:
      pacWebhookInsecureSSL: true
```

> **Warning:** Only use this in test or development environments. Production
> deployments should use properly signed certificates.
