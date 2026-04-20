---
title: "GitHub Application Secrets"
linkTitle: "GitHub Application Secrets"
weight: 2
description: "Creating a GitHub App and deploying its credentials as secrets in the cluster."
---

Konflux uses a GitHub App for triggering pipelines via webhooks and for interacting
with repositories (creating PRs, reporting status). You need to create a GitHub App
and deploy its credentials as secrets in the cluster.

## Creating a GitHub App

Create a GitHub App following the
[Pipelines-as-Code documentation](https://pipelinesascode.com/docs/providers/github-app/#manual-setup).

{{% alert color="info" %}}
That tutorial asks you to generate and set a **Webhook secret** when creating the App.
The same value should be used in the App and for `WEBHOOK_SECRET` in `deploy-local.env`.

Generate a random secret running: `head -c 30 /dev/random | base64`.
{{% /alert %}}

For `Homepage URL` you can use `https://localhost:9443/` (it doesn't matter).

For `Webhook URL`, use either:
- Your cluster's publicly reachable ingress URL, if available
- A [smee](https://smee.io/) webhook proxy URL, if the cluster is not reachable from
  the internet (see
  [Webhook Proxy for Non-Exposed Clusters](#webhook-proxy-for-non-exposed-clusters) below)

Per the instructions on the link, generate and download the private key. Take note of
the location of the private Key, the App ID and the webhook secret you set in the App
(random value generated above).

If using a local cluster, set these values in `deploy-local.env`:
- **GITHUB_PRIVATE_KEY_PATH**: path to private key downloaded earlier
- **WEBHOOK_SECRET**: secret generated earlier
- **GITHUB_APP_ID**: GitHub APP ID

If deploying to a remote cluster, refer to the [section below](#creating-the-secrets).

Install the GitHub App on the repositories you want to use with Konflux.

## Webhook Proxy for Non-Exposed Clusters

When deployed in a local environment like Kind, or behind a firewall, GitHub cannot
reach the cluster's webhook endpoint directly. Use [smee](https://smee.io/) as a
proxy to relay webhook events into the cluster.

Generate a smee channel ID with
`head -c 30 /dev/random | base64 | tr -dc 'a-zA-Z0-9'`, then use
`https://smee.io/<channel-id>` (with that output as `<channel-id>`) as the
**Webhook URL** when creating or configuring your GitHub App, and set the same URL
as `SMEE_CHANNEL` in `scripts/deploy-local.env`. The `deploy-local.sh` script
configures the smee client to listen on that channel. Alternatively, create a
channel at [smee.io](https://smee.io/) and use the URL it gives you.

## Creating the Secrets

The same GitHub App secret must be created in three namespaces so that all Konflux
components can interact with GitHub:

```bash
for ns in pipelines-as-code build-service integration-service; do
  kubectl -n "${ns}" create secret generic pipelines-as-code-secret \
    --from-file=github-private-key=/path/to/github-app.pem \
    --from-literal=github-application-id="<your-app-id>" \
    --from-literal=webhook.secret="<your-webhook-secret>"
done
```

The `deploy-local.sh` script creates these secrets automatically from the values
in `scripts/deploy-local.env`.
