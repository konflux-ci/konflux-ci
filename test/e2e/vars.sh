#!/bin/bash -e
export E2E_TEST_IMAGE \
CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_LOCALHOST CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_CLUSTER \
CUSTOMIZED_DOCKER_OCI_TA_PIPELINE_IMAGE_REF_LOCALHOST CUSTOMIZED_DOCKER_OCI_TA_PIPELINE_IMAGE_REF_CLUSTER \
CUSTOMIZED_BUILD_TASK_IMAGE_REF_LOCALHOST CUSTOMIZED_BUILD_TASK_IMAGE_REF_CLUSTER
# https://github.com/konflux-ci/e2e-tests/commit/<COMMIT>
E2E_TEST_IMAGE=quay.io/redhat-user-workloads/konflux-qe-team-tenant/konflux-e2e/konflux-e2e-tests:on-pr-13374abfa112baa5bd035f30cc7771d327b6143f
CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_LOCALHOST="localhost:30001/test/test:customized-docker-pipeline"
CUSTOMIZED_DOCKER_PIPELINE_IMAGE_REF_CLUSTER="registry-service.kind-registry/test/test:customized-docker-pipeline"
CUSTOMIZED_DOCKER_OCI_TA_PIPELINE_IMAGE_REF_LOCALHOST="localhost:30001/test/test:customized-docker-oci-ta-pipeline"
CUSTOMIZED_DOCKER_OCI_TA_PIPELINE_IMAGE_REF_CLUSTER="registry-service.kind-registry/test/test:customized-docker-oci-ta-pipeline"

CUSTOMIZED_BUILD_TASK_IMAGE_REF_LOCALHOST="localhost:30001/test/test:customized-build-task"
CUSTOMIZED_BUILD_TASK_IMAGE_REF_CLUSTER="registry-service.kind-registry/test/test:customized-build-task"
