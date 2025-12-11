#!/bin/bash
set -euo pipefail

# Assume this sets the env vars mentioned below.
# See example.env for an example.
source .env

# I'm using user-ns2, so that's where I'll add the credential.
# Adjust as needed.
for namespace in user-ns2; do
  kubectl create -n "$namespace" secret generic regcred \
          --from-file=.dockerconfigjson="$QUAY_CREDENTIAL_FILE" \
          --type=kubernetes.io/dockerconfigjson
done
