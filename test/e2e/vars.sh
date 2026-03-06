#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:422ebb8a28b9400df5b588c3e308a4f3bf36cd0a6604e241d65ecd3876daf94b

export RELEASE_SERVICE_CATALOG_REVISION
# renovate: datasource=git-refs depName=https://github.com/konflux-ci/release-service-catalog
RELEASE_SERVICE_CATALOG_REVISION="9c86e740ec30080dd568ecb313335c6c45c4dc50"
