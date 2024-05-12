# Releasing Guidelines

Releases for this repository are managed by a
[github action](./.github/workflows/release.yml), and are triggered when a tag is pushed
to the repository.

Each release contains a tar archive of the repository at the point in which the release
was created.

To create a release:

1. Create a new branch at the position you want to create a release (e.g. branch,
   commit).

2. Create a tag at that branch.

3. Push the tag to the upstream repository.

## Example:

```bash
git fetch upstream main
git checkout -b v0.1 upstream/main
git tag v0.1 v0.1
git push upstream tag v0.1
```
