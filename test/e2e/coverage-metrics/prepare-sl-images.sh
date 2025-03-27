#!/bin/bash

export SEALIGHTS_TOKEN="${SEALIGHTS_TOKEN:-""}"
export SEALIGHTS_LAB_ID="${SEALIGHTS_LAB_ID:-""}"
export TMP_FOLDER="$(mktemp -d)"

# Define array with core components and the path in the root repo
SERVICES_ENTRIES=(
  "integration:konflux-ci/integration/core/kustomization.yaml"
  "release:konflux-ci/release/core/kustomization.yaml"
  "build-service:konflux-ci/build-service/core/kustomization.yaml"
  "image-controller:konflux-ci/image-controller/core/kustomization.yaml"
)

for service_entry in "${SERVICES_ENTRIES[@]}"; do
  component_name="${service_entry%%:*}"  # Extract service name (before ':')
  kustomization_file="${service_entry#*:}"  # Extract file path (after ':')

  echo "[INFO] Processing component: $component_name from kustomization file $kustomization_file"

  PRISTINE_IMAGE=$(yq -r '.images[] | "\(.newName):\(.newTag)"' "$kustomization_file")

  if [ -z "$PRISTINE_IMAGE" ]; then
    echo "[INFO] No image found in $kustomization_file, skipping..."
    continue
  fi

  echo "[INFO] Current pristine image: $PRISTINE_IMAGE"

  # Download the image and fetch attestation using cosign
  cosign download attestation "$PRISTINE_IMAGE" > "$TMP_FOLDER/cosign_${component_name}_metadata.json"

  # Extract SOURCE_ARTIFACT from attestation metadata
  SL_SOURCE_ARTIFACT=$(jq -r '
    .payload | @base64d | fromjson | .predicate.buildConfig.tasks[] |
    select(.invocation.environment.labels."konflux-ci/sealights" == "true") |
    .results[] | select(.name == "SOURCE_ARTIFACT") | .value' "$TMP_FOLDER/cosign_${component_name}_metadata.json")

  if [ -z "$SL_SOURCE_ARTIFACT" ]; then
    echo "[ERROR] No SOURCE_ARTIFACT found, skipping..."
    continue
  fi

  # Extract IMAGE_REF (which contains full Sealights image reference)
  SL_CONTAINER_IMAGE=$(jq -r --arg sl_source_artifact "$SL_SOURCE_ARTIFACT" '
    .payload | @base64d | fromjson | .predicate.buildConfig.tasks[] |
    select(.invocation.parameters.SOURCE_ARTIFACT == $sl_source_artifact) |
    select(.ref.params[].value == "buildah-oci-ta") | .results[] | select(.name == "IMAGE_REF") |
    .value' "$TMP_FOLDER/cosign_${component_name}_metadata.json")

  if [ -z "$SL_CONTAINER_IMAGE" ]; then
    echo "[ERROR] No IMAGE_REF found in attestation, skipping..."
    continue
  fi

  # Extract sealights image name and tag separately
  SL_IMAGE_NAME=$(echo "$SL_CONTAINER_IMAGE" | cut -d':' -f1)
  SL_IMAGE_TAG=$(echo "$SL_CONTAINER_IMAGE" | cut -d':' -f2 | cut -d'@' -f1)

  echo "[INFO] Extracted Image Name: $SL_IMAGE_NAME"
  echo "[INFO] Extracted Image Tag: $SL_IMAGE_TAG"

  echo "[INFO] Updating kustomization file: $kustomization_file"

  yq -i ".images[0].newName = \"$SL_IMAGE_NAME\" | .images[0].newTag = \"$SL_IMAGE_TAG\"" "$kustomization_file"

  echo "[INFO] Updated kustomization file: $kustomization_file"

  yq e "
  (.spec.template.spec.containers[].env[] | select(.name == \"SEALIGHTS_LAB_ID\")).value = \"${SEALIGHTS_LAB_ID}\" |
  (.spec.template.spec.containers[].env[] | select(.name == \"SEALIGHTS_TOKEN\")).value = \"${SEALIGHTS_TOKEN}\"
" -i konflux-ci/"$component_name"/core/sealights-envs.yaml
done
