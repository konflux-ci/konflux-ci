#!/bin/bash
set -euo pipefail

# This can be used to destroy and rebuild a working cluster locally with
# kind and a quay.io registry. Expects an .env file to exist and to define
# some required secrets, including credentials for GitHub and Quay.

function slow-title() {
  sleep 2
  echo ""
  echo "************************************************************"
  echo " 🐢 $1"
  echo "************************************************************"
  echo ""
}

slow-title "Deleting old cluster"
kind delete cluster --name konflux

slow-title "Creating cluster"
kind create cluster --name konflux --config kind-config.yaml

slow-title "Deploying dependencies"
./deploy-deps.sh

slow-title "Deploying Konflux"
./deploy-konflux.sh

slow-title "Deploying demo users"
./deploy-test-resources.sh

slow-title "Deploying PAC secret"
./deploy-pac-github-secret.sh

slow-title "Deploying image controller"
(source .env && ./deploy-image-controller.sh "$QUAY_TOKEN" "$QUAY_ORG")

slow-title "Deploying Quay secret"
./deploy-quay-push-secret.sh

slow-title "Ready"
echo "https://localhost:9443/"
