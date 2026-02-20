Configuring GitHub Application Secrets
===

Konflux uses a GitHub App for triggering pipelines via webhooks and for interacting
with repositories (creating PRs, reporting status). You need to create a GitHub App
and deploy its credentials as secrets in the cluster.

<!-- toc -->

- [Creating a GitHub App](#creating-a-github-app)
- [Webhook Proxy for Non-Exposed Clusters](#webhook-proxy-for-non-exposed-clusters)
- [Creating the Secrets](#creating-the-secrets)

<!-- tocstop -->

# Creating a GitHub App

:gear: Create a GitHub App following the
[Pipelines-as-Code documentation](https://pipelinesascode.com/docs/install/github_apps/#manual-setup).

For `Homepage URL` you can use `https://localhost:9443/` (it doesn't matter).

For `Webhook URL`, use either:
- Your cluster's publicly reachable ingress URL, if available
- A [smee](https://smee.io/) webhook proxy URL, if the cluster is not reachable from
  the internet (see [below](#webhook-proxy-for-non-exposed-clusters))

:gear: Per the instructions on the link, generate and download the private key. Take
note of the App ID and the webhook secret you configure during the process. You will
need these values to create the cluster secrets.

:gear: Install the GitHub App on the repositories you want to use with Konflux.

# Webhook Proxy for Non-Exposed Clusters

When deployed in a local environment like Kind, or behind a firewall, GitHub cannot
reach the cluster's webhook endpoint directly. Use [smee](https://smee.io/) as a
proxy to relay webhook events into the cluster.

The `deploy-local.sh` script handles smee configuration automatically. Set the
`SMEE_CHANNEL` variable in `scripts/deploy-local.env` to use a specific channel, or
leave it empty to have one generated automatically. Use the smee channel URL as the
`Webhook URL` when creating or configuring your GitHub App.

For manual deployments, see the smee
[documentation](https://github.com/probot/smee-client) for deploying a client to
your cluster.

# Creating the Secrets

The same GitHub App secret must be created in three namespaces so that all Konflux
components can interact with GitHub:

:gear: Create the secrets using the values from your GitHub App:

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
