#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:1881c97841ec44d19c0a8a8e9243f8a8de0ba780182f9449994375a1b8f24a39
