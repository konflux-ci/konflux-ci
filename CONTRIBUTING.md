Contributing Guidelines
===

<!-- toc -->

- [Editing Markdown Files](#editing-markdown-files)
- [Using KubeLinter](#using-kubelinter)
- [Running E2E test](#running-e2e-test)
  * [Step 1: Deploy the Konflux Environment](#step-1-deploy-the-konflux-environment)
  * [Step 2: Configure the E2E Test Runner](#step-2-configure-the-e2e-test-runner)
  * [Step 3: Run the Test](#step-3-run-the-test)

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
To validate changes, you can run the end-to-end (E2E) test suite which validates the core Konflux user journey, from creating an application to releasing it.

## Step 1: Deploy the Konflux Environment
This is the most important prerequisite. The test suite runs against a live, deployed Konflux instance.

:gear: **Follow the complete `Bootstrapping the Cluster` guide in the `README.md` file.**

This includes:
- Configuring your `dependencies/smee/smee-channel-id.yaml`.
- Configuring your main deployment environment file at `scripts/deploy-e2e.env` with your GitHub App secrets and Quay token.
- Running `./scripts/deploy-e2e.sh` to bring the entire environment online.

## Step 2: Configure the E2E Test Runner
The test runner script needs its own configuration to know how to interact with your deployed environment (e.g., which GitHub repository to open a PR against). This configuration is kept separate from the main deployment secrets.

:gear: **Create and fill out the test environment file**:
  - Copy the template: `cp test/e2e/run-e2e.env.template test/e2e/run-e2e.env`
  - Edit `test/e2e/run-e2e.env` and provide the required values.

*(Alternative)*: You can also `export` these variables into your shell. The script will use exported variables before falling back to the `.env` file.

## Step 3: Run the Test
Once your environment is deployed and your test runner is configured, you can execute the test suite.

:gear: **From the root of the repository, run the command**:
```bash
./test/e2e/run-e2e.sh
```
The source code for the test is located at https://github.com/konflux-ci/e2e-tests/tree/main/tests/konflux-demo.
