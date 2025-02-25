#!/bin/bash -e
export E2E_TEST_IMAGE CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_LOCALHOST CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_CLUSTER CUSTOMIZED_BUILD_TASK_IMAGE_REF_LOCALHOST CUSTOMIZED_BUILD_TASK_IMAGE_REF_CLUSTER
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests@sha256:d84e9d4e5932bdb28acc54bf404e840d2c77afe55f79bfc26a4b5a048ac1e81b
CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_LOCALHOST="localhost:30001/test/test:customized-docker-pipeline"
CUSTOMIZED_BUILD_TASK_IMAGE_REF_LOCALHOST="localhost:30001/test/test:customized-build-task"
CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_CLUSTER="registry-service.kind-registry/test/test:customized-docker-pipeline"
CUSTOMIZED_BUILD_TASK_IMAGE_REF_CLUSTER="registry-service.kind-registry/test/test:customized-build-task"
