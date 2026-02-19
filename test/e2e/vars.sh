#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:2e59be294d1b8a9c50bb3c8139608b4623ecdfa9c2695308d311582c12c42bda

export RELEASE_SERVICE_CATALOG_REVISION
# renovate: datasource=git-refs depName=https://github.com/konflux-ci/release-service-catalog
RELEASE_SERVICE_CATALOG_REVISION="ae6c88474b0dba4815323fff8fc454b99d09238a"
