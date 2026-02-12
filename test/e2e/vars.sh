#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:bddeef258ebb6c5dd48d6b9caa6434ab79be4e6ecbd0c1b5bb10dd5b557cb1ae

export RELEASE_SERVICE_CATALOG_REVISION
# renovate: datasource=git-refs depName=https://github.com/konflux-ci/release-service-catalog
RELEASE_SERVICE_CATALOG_REVISION="f7c943075e1ed7956068960c9c6006553b94003c"
