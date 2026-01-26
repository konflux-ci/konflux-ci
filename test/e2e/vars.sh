#!/bin/bash -e
# TEMPORARY: verification build for TA-secret fix. Revert to default after verifying.
# Pin: quay.io/yftacherzog/konflux-e2e-tests:ta-secret-verify — tag your build with "ta-secret-verify".
E2E_TEST_IMAGE="quay.io/yftacherzog/konflux-e2e-tests:ta-secret-verify"
# Default (restore after verification):
# E2E_TEST_IMAGE="quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:faf0ce7d00f1f100bb8bb289dfbfb6548033a8a6cd7faa74d142fb60cd14b73c"
export E2E_TEST_IMAGE

# renovate: datasource=git-refs depName=https://github.com/konflux-ci/release-service-catalog
RELEASE_SERVICE_CATALOG_REVISION="1025ec2df85e045adec718e205a43f1262e40857"
export RELEASE_SERVICE_CATALOG_REVISION
