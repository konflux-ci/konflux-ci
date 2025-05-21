#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:da7aae4a78953c6a602f769d912317e2aea5c696944a646eedb3edf1ac0ed94f
