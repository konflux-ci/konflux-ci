#!/bin/bash -e
export E2E_TEST_IMAGE
# https://github.com/konflux-ci/e2e-tests/commit/3a0db9b478e9cb629623f932d7e163d10726add3
# Using multi-arch image tag that supports both AMD64 and ARM64
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests:on-pr-3a0db9b478e9cb629623f932d7e163d10726add3
