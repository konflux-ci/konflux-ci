# Hosted URL and GitHub SSO Configuration Guide

## Overview

The default Konflux deployment is designed for local development and assumes the UI is accessed at:

https://localhost:9443

When deploying Konflux using a hosted/public domain (for example: https://konflux.example.com) and enabling GitHub SSO, multiple components must be updated. All references to `localhost:9443` must be replaced with your public domain.

This document explains exactly what must be changed.

---

## Components That Require Updates

For hosted deployments, update the following:

1. Dex configuration  
2. OAuth2 Proxy configuration  
3. Pipelines as Code (PaC) console URLs  
4. TLS certificates  
5. GitHub OAuth App settings  

Failure to update all components consistently will result in authentication errors.

---

## 1. Update Dex Configuration

File:

dependencies/dex/config.yaml

Default configuration:

issuer: https://localhost:9443/idp/

staticClients:
- id: oauth2-proxy
  redirectURIs:
  - 'https://localhost:9443/oauth2/callback'

Replace `localhost:9443` with your hosted domain:

issuer: https://konflux.example.com/idp/

staticClients:
- id: oauth2-proxy
  redirectURIs:
  - 'https://konflux.example.com/oauth2/callback'

Important:
- The `issuer` must exactly match the OAuth2 Proxy `--oidc-issuer-url`.
- The redirect URI must exactly match the GitHub OAuth callback URL.

---

## 2. Update OAuth2 Proxy Configuration

File:

konflux-ci/ui/core/proxy/proxy.yaml

Locate these arguments:

--redirect-url https://localhost:9443/oauth2/callback
--oidc-issuer-url https://localhost:9443/idp/
--login-url https://localhost:9443/idp/auth
--whitelist-domain localhost:9443

Replace them with:

--redirect-url https://konflux.example.com/oauth2/callback
--oidc-issuer-url https://konflux.example.com/idp/
--login-url https://konflux.example.com/idp/auth
--whitelist-domain konflux.example.com

All of these values must match the public hostname used to access Konflux.

---

## 3. Update Pipelines as Code Console URLs

File:

dependencies/pipelines-as-code/custom-console-patch.yaml

Replace all instances of:

https://localhost:9443

With:

https://konflux.example.com

This ensures pull request links, namespace links, and pipeline log links redirect correctly.

---

## 4. Update TLS Certificates

### Dex Certificate

File:

dependencies/dex/dex.yaml

Default:

dnsNames:
- localhost

Change to:

dnsNames:
- konflux.example.com


### UI Serving Certificate

File:

operator/upstream-kustomizations/ui/certmanager/certificate.yaml

Default:

dnsNames:
- localhost

Replace with:

dnsNames:
- konflux.example.com

Certificates must include your public DNS name or browsers will reject TLS connections.

---

## 5. Update GitHub OAuth Application

In GitHub:

Settings → Developer Settings → OAuth Apps

Configure:

Homepage URL:
https://konflux.example.com

Authorization callback URL:
https://konflux.example.com/oauth2/callback

The callback URL must exactly match the OAuth2 Proxy `--redirect-url`.

---

## Common Errors

redirect_uri_mismatch  
Cause: GitHub OAuth callback URL does not match proxy redirect URL.  
Fix: Ensure both use the exact same hosted domain.

invalid issuer  
Cause: Dex `issuer` does not match OAuth2 Proxy `--oidc-issuer-url`.  
Fix: Ensure both values are identical.

Cookie domain errors  
Cause: `--whitelist-domain` still set to localhost.  
Fix: Update to your public hostname.

TLS certificate errors  
Cause: Certificates do not include your public DNS name.  
Fix: Update `dnsNames` and reissue certificates.

---

## Summary

For hosted deployments, replace all references to:

https://localhost:9443

with your public domain across:

- Dex configuration  
- OAuth2 Proxy arguments  
- Pipelines as Code console URLs  
- TLS certificate DNS entries  
- GitHub OAuth settings  

Authentication will fail if any component still references localhost.