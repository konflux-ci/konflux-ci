<div align="center">
  <a href="https://konflux-ci.dev">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="assets/konflux.svg">
      <source media="(prefers-color-scheme: light)" srcset="assets/konflux-light.svg">
      <img src="assets/konflux-light.svg" alt="Konflux" width="300">
    </picture>
  </a>
</div>

<p align="center">
  <strong>Trusted builds made easy</strong><br>
  A cloud-native software factory for building, testing, and releasing trusted software artifacts
</p>

<p align="center">
  <a href="https://github.com/konflux-ci/konflux-ci/releases/latest"><img src="https://img.shields.io/github/v/release/konflux-ci/konflux-ci?style=flat-square&label=Latest%20Release&color=blue" alt="Latest Release"></a>
  <a href="https://goreportcard.com/report/github.com/konflux-ci/konflux-ci/operator"><img src="https://goreportcard.com/badge/github.com/konflux-ci/konflux-ci/operator?style=flat-square" alt="Go Report Card"></a>
  <a href="https://join.slack.com/t/konflux-ci/shared_invite/zt-3g36o0a4x-Dmsy25XEGuBV79S7kd04CA"><img src="https://img.shields.io/badge/Slack-Join%20Chat-4a154b?style=flat-square&logo=slack" alt="Slack"></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue?style=flat-square" alt="License"></a>
</p>

<p align="center">
  <a href="https://konflux-ci.dev/">Website</a> &bull;
  <a href="https://konflux-ci.dev/docs/">User Docs</a> &bull;
  <a href="https://konflux-ci.dev/konflux-ci/docs/">Admin Docs</a> &bull;
  <a href="https://konflux-ci.dev/architecture/">Architecture</a> &bull;
  <a href="https://konflux-ci.dev/konflux-ci/docs/installation/install-local/">Getting Started</a>
</p>

---

## What is Konflux?

Konflux is an open-source, Kubernetes-native CI/CD platform that manages the full software
delivery lifecycle for software artifacts — with supply chain trust built in
from the start. Built on [Tekton](https://tekton.dev/) and the
[Conforma](https://enterprisecontract.dev/) policy framework, it brings together
best-in-class open source projects into a single, integrated software factory.
Managed by a Kubernetes operator, Konflux runs on Kind, OpenShift, and any conformant Kubernetes cluster.

## Try Konflux

Want to see it in action? You can have a full Konflux instance running on a local Kind
cluster in just a few minutes — no cloud account, no complex setup.
Jump into the [Local Deployment Guide](https://konflux-ci.dev/konflux-ci/docs/installation/install-local/) and start building!

## Key Features

| | Feature | Description |
|---|---|---|
| **Build** | Automated Pipelines | Triggers Tekton pipelines on every pull request and merge, producing signed container images with attached SBOMs |
| **Test** | Integration Testing | Runs integration test scenarios after each build, using pluggable pipelines and Conforma policies to gate releases |
| **Release** | Managed Releases | Orchestrates releases to target registries through declarative configuration |
| **Trust** | Supply Chain Trust | Every artifact is signed with cosign, attested with SLSA provenance, and policy-checked before release |

## Ecosystem

Konflux integrates with leading open-source projects out of the box, while remaining
flexible enough to work with your preferred tools:

- **Pipelines** — [Tekton](https://tekton.dev/)
- **Builds** — [Buildah](https://buildah.io/), [Hermeto](https://github.com/hermetoproject) (prefetching content for network-isolated builds)
- **Trust & Signing** — [Sigstore](https://sigstore.dev/), [Conforma](https://enterprisecontract.dev/)
- **Scanning & SBOMs** — [Clair](https://quay.github.io/clair/), [ClamAV](https://www.clamav.net/), [Trustify](https://www.trustification.io/) (SBOM storage & [dependency analytics](https://github.com/guacsec/trustify-dependency-analytics))
- **Registry** — [Quay](https://www.projectquay.io/), [Zot](https://zotregistry.dev/) (or any OCI-compatible registry)
- **Dependency Updates** — [Renovate (Mintmaker)](https://github.com/konflux-ci/mintmaker)
- **Scheduling** — [Kueue](https://kueue.sigs.k8s.io/) (PipelineRun queuing & scheduling)
- **Authentication** — [Dex](https://dexidp.io/) (with support for OIDC, GitHub, LDAP, and more)

## Contributing

We welcome contributions from the community! Whether it's bug reports, feature requests,
documentation improvements, or code contributions — every bit helps.

- Read our [Contributing Guide](./CONTRIBUTING.md) to get started
- Join us on [Slack](https://join.slack.com/t/konflux-ci/shared_invite/zt-3g36o0a4x-Dmsy25XEGuBV79S7kd04CA) to chat with the community
- Check out our [open issues](https://github.com/konflux-ci/konflux-ci/issues) for ways to contribute
- [Report a bug or request a feature](https://github.com/konflux-ci/konflux-ci/issues/new)
- Found a security vulnerability? Please report it privately via [GitHub's security advisories](https://github.com/konflux-ci/konflux-ci/security/advisories/new)

## Release Process

See [RELEASE.md](./RELEASE.md) for the release process and versioning policy.

## License

Konflux is licensed under the [Apache License 2.0](./LICENSE).
