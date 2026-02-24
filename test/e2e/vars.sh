#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:3dd9a96e4119a8d672f3ae65cc8b8a341fecf22cc2043e9b626478a23d397d57

export RELEASE_SERVICE_CATALOG_REVISION
# renovate: datasource=git-refs depName=https://github.com/konflux-ci/release-service-catalog
RELEASE_SERVICE_CATALOG_REVISION="6049afb7ec1ab776bd869dd42bb6d211bd9cd8fc"
