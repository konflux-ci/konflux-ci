Contributing Guidelines
===

<!-- toc -->

- [Editing Markdown Files](#editing-markdown-files)
- [Using KubeLinter](#using-kubelinter)
- [Running E2E test](#running-e2e-test)
  * [Prerequisites](#prerequisites)
  * [Setup](#setup)
  * [Running the test](#running-the-test)

<!-- tocstop -->

# Editing Markdown Files

If the structure of markdown files containing table of contents changes, those
need to be updated as well.

To do that, run the command below and add the produced changes to your PR.

```bash
find . -name "*.md" | while read -r file; do
    npx markdown-toc $file -i
done
```

# Using KubeLinter

Please consider running [KubeLinter](https://docs.kubelinter.io/#/?id=usage)
locally before submitting a PR to this repository.

After [installing KubeLinter](https://docs.kubelinter.io/#/?id=installing-kubelinter)
and adding it to the $PATH env variable, create a new folder in the base directory 
using `mkdir -p ./.kube-linter/`. Then, run the following Bash script:
```
    find . -name "kustomization.yaml" -o -name "kustomization.yml" | while read -r file; do
    dir=$(dirname "$file")
    dir=${dir#./}
    output_file=$(echo "out-$dir" | tr "/" "-")
    kustomize build "$dir" > "./.kube-linter/$output_file.yaml"
    done
```
finally, run `kube-linter lint ./.kube-linter` to recursively apply KubeLinter checks on this folder.

It may be also recommended to create a configuration file. To do so please check
[KubeLinter config documentation](https://docs.kubelinter.io/#/configuring-kubelinter)
this file will allow you to ignore or include specific KubeLinter checks.

# Running E2E test
In order to validate changes quicker, it is possible to run E2E test, which validates that:
* Application and Component can be created
* Build PipelineRun is triggered and can finish successfully
* Integration test gets triggered and finishes successfully
* Application Snapshot can be released successfully

## Prerequisites
* Fork of https://github.com/konflux-ci/testrepo is created and your GitHub App is installed there
* Konflux is deployed on `kind` cluster (follow the guide in README)
* quay.io organization that has `test-images` repository created, with robot account that has admin access to that repo

## Setup
Export following environment variables
```
# quay.io org where the built and released image will be pushed to
export QUAY_ORG="" \
# quay.io org OAuth access token
QUAY_TOKEN="" \
# Content of quay.io credentials config generated (ideally) for the robot account 
# that has access to $QUAY_ORG/test-images repository
QUAY_DOCKERCONFIGJSON="$(< /path/to/docker/config.json)" \
# Your GitHub App's ID
APP_ID="" \
# Your GitHub App's private key
APP_PRIVATE_KEY="" \
# Your GitHub App's webhook secret
APP_WEBHOOK_SECRET="" \
# URL of the smee.io channel you created
SMEE_CHANNEL="" \
# Name of the GitHub org/username where the https://github.com/konflux-ci/testrepo is forked
GH_ORG="" \
# GitHub token with permissions to merge PRs in your GH_ORG
GH_TOKEN=""
```

## Running the test

Run (from the root of the repository directory):
```
./deploy-test-resources.sh
./test/e2e/prepare-e2e.sh
./test/e2e/run-e2e.sh
```

The source code of the test is located [.](https://github.com/konflux-ci/e2e-tests/tree/main/tests/konflux-demo)