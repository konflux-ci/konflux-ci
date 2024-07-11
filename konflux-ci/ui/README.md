# UI

## Overview

This component will deploy the UI.

`proxy` - Forwards requests to the kube api, and redirects the user to perform
authentication if the user doesn't have a valid cookie.

## Dependencies

`Dex` is required to be present for `oauth2-proxy` to be deployed.
