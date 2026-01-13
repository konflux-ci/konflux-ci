# Konflux Operator Documentation

This directory contains the documentation for the Konflux Operator, configured for GitHub Pages using Hugo with the Docsy theme.

## Local Development

To view the documentation locally:

1. Install Hugo (extended version recommended):
   ```bash
   # Using Go (if you have Go installed):
   go install -tags extended github.com/gohugoio/hugo@latest

   # Or download from: https://gohugo.io/installation/
   # On macOS (with Homebrew):
   brew install hugo
   # On Fedora/RHEL:
   sudo dnf install hugo
   # On Ubuntu/Debian:
   sudo apt-get install hugo
   ```

2. Install Node.js (required for PostCSS processing in Docsy theme):
   ```bash
   # Check if Node.js is already installed:
   node --version
   npm --version

   # If not installed, download from: https://nodejs.org/
   # On macOS (with Homebrew):
   brew install node
   # On Fedora/RHEL:
   sudo dnf install nodejs npm
   # On Ubuntu/Debian:
   sudo apt-get install nodejs npm
   ```

3. Install Hugo module dependencies:
   ```bash
   cd operator/docs
   hugo mod tidy
   ```
   This will download the Docsy theme and its dependencies.

4. Generate documentation and serve:
   ```bash
   cd operator
   make generate-docs
   make docs-serve
   ```

5. Open http://localhost:4000/konflux-ci/operator/ in your browser

## Generating Documentation

The documentation is generated from:
- API types in `operator/api/v1alpha1/` (using `genref`)
- Sample YAML files in `operator/config/samples/`

To regenerate:
```bash
cd operator
make generate-docs
```

## GitHub Pages Deployment

The documentation is automatically deployed to GitHub Pages via GitHub Actions:

- **Pull Requests**: The workflow builds and verifies the documentation to check for errors (no preview is deployed, as GitHub Pages doesn't support PR previews)
- **Main Branch**: When changes are pushed to `main`, the documentation is automatically built and deployed to GitHub Pages

The workflow is configured in `.github/workflows/pages.yml`.

**Note**: To preview documentation changes locally before creating a PR, use `make docs-serve` (see [Local Development](#local-development) above).

## Structure

- `index.md` - Homepage with examples
- `reference/konflux.v1alpha1.md` - API reference documentation
- `config.yaml` - Hugo configuration
- `go.mod` - Hugo module dependencies (Docsy theme)
- `static/` - Static assets (images, CSS)
