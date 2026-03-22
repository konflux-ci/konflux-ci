# Konflux CI

Konflux is a cloud-native, Kubernetes-native CI/CD platform built on top of
[Tekton](https://tekton.dev/), and the
[Conforma](https://enterprisecontract.dev/) policy framework.

It provides a unified, self-service experience for building, testing, and releasing
container images — with supply chain security built in from the start.

## What Konflux Does

Konflux manages the full software delivery lifecycle for containerized applications:

- **Build** — Triggers Tekton pipelines on every pull request and merge, producing
  signed container images with attached SBOMs.
- **Test** — Runs integration test scenarios after each build, using pluggable
  pipelines and Enterprise Contract policies to gate releases.
- **Release** — Orchestrates releases to target registries through a declarative
  `ReleasePlan` / `ReleasePlanAdmission` model, separating developer and operations
  concerns.
- **Secure by default** — Every artifact is signed with cosign, attested with SLSA
  provenance, and policy-checked before release.

Konflux is deployed and managed by the **Konflux Operator**, a Kubernetes operator
that provisions and reconciles all platform components from a single `Konflux` Custom
Resource. It supports local development environments (Kind), OpenShift, and any
Kubernetes cluster — with OLM, release bundle, or source-based installation.

## Documentation

The full **operator and administrator documentation** is published at:
**[https://konflux-ci.dev/konflux-ci/docs/](https://konflux-ci.dev/konflux-ci/docs/)**

For **end-user documentation** (how to use Konflux to build, test, and release applications), see:
**[https://konflux-ci.dev/docs/](https://konflux-ci.dev/docs/)**


## Contributing

Contributions are welcome. See [CONTRIBUTING.md](./CONTRIBUTING.md) for guidelines.

## Release Process

See [RELEASE.md](./RELEASE.md) for the release process and versioning policy.
