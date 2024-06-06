# UI

## Overview

This component will deploy the UI.
There are four components for the ui.

[chrome](https://github.com/RedHatInsights/insights-chrome) - The main frontend component.

[hac](https://github.com/openshift/hac-core) - A plugin for `chrome` (loaded by `chrome`).

[hac-dev](https://github.com/openshift/hac-dev) - A plugin for `hac` (loaded by `hac`).

`proxy` - Forwards requests to the kube api, and hosts
static files required by the chrome component
(used instead of deploying the entire [chrome backend](https://github.com/RedHatInsights/chrome-service-backend). The static files are copied from the `chrome` image into an empty volume
that is served by the proxy.

**Note**: The frontend assumes that all the static files and api calls are made to the same host.


## Dependencies

The `chrome` component requires a `Keycloak` instance for authentication.

## Customizations

It's required to customize the manifests before deploying them. Two customizations are needed:

1. Updating the hostname that will be used for the routes.

2. Creating a `fed-modules.json` file that configures the frontend. **Important**: this file contains the `Keycloak` endpoint that will be used for authentication.
