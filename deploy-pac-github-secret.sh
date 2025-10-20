#!/bin/bash
set -euo pipefail

# Assume this sets the env vars mentioned below.
# See example.env for an example.
source .env

# Create the Pipelines as Code secret in three places
for namespace in pipelines-as-code build-service integration-service; do
  kubectl -n $namespace create secret generic pipelines-as-code-secret \
          --from-literal github-application-id="$APP_ID" \
          --from-literal github-private-key="$(cat "$PATH_PRIVATE_KEY")" \
          --from-literal webhook.secret="$WEBHOOK_SECRET"
done
