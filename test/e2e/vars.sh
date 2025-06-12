#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:336072bf011c0a081527d24728280a8990f7f68d1c6f84a68c1f7c3bcdd38d9f
