#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:61dc1f5981e4ba0004de803bfffb2bc513dc5855c212457282d12975b9cfa7ca
