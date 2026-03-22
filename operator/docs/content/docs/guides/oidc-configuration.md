---
title: "Authentication and OIDC Configuration"
linkTitle: "Authentication and OIDC Configuration"
weight: 4
description: "Configuring login providers for Konflux using Dex connectors - GitHub, OpenShift, OIDC, LDAP, and static passwords."
---

Konflux uses [Dex](https://dexidp.io/) as a federated identity broker and
[oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy) to authenticate users
against one or more third-party identity providers. The operator manages both components
and exposes their configuration through the Konflux CR.

All authentication settings live under `spec.ui.spec.dex` in the `Konflux` CR.

## Overview

Authentication in Konflux works as follows:

1. The user's browser is redirected to Dex.
2. Dex presents the configured connectors (GitHub, OpenShift, OIDC, LDAP, etc.) as
   login options.
3. After the user authenticates with an upstream provider, Dex issues a token to
   oauth2-proxy.
4. oauth2-proxy validates the token and grants access to the Konflux UI.

The `spec.ui.spec.dex.config` section controls which identity providers are available
and how Dex is configured.

{{< alert color="warning" >}}
The static-password configuration included in the default sample CR (and used in local
Kind deployments) is intended for <strong>development and CI only</strong>. Remove
<code>staticPasswords</code> and configure an OIDC connector before deploying to
production.
{{< /alert >}}

## GitHub OAuth

GitHub OAuth is the a common connector for Konflux deployments.

### Creating a GitHub OAuth App

Create a GitHub OAuth App following the
[GitHub documentation](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/creating-an-oauth-app).

When registering the app, set the **Authorization callback URL** to:

```
https://<your-konflux-hostname>/idp/callback
```

Dex is not exposed at a separate hostname — it runs behind the Konflux proxy at the `/idp/`
path of your Konflux UI URL. The operator derives this URL automatically from `ingress.host`
(or the OpenShift default ingress domain if not explicitly set).

Once created, note the **Client ID** and generate a **Client Secret** — you will need both in the next step.

### Creating the Secret

Store the credentials in the `konflux-ui` namespace where Dex runs:

```bash
kubectl create secret generic github-client \
  --namespace konflux-ui \
  --from-literal=clientID="<your-client-id>" \
  --from-literal=clientSecret="<your-client-secret>"
```

### Configuring the Connector

Reference the secret via environment variables in the `dex` container and add a
`github` connector to the Dex configuration:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  ui:
    spec:
      ingress:
        enabled: true
      dex:
        dex:
          env:
            - name: GITHUB_CLIENT_ID
              valueFrom:
                secretKeyRef:
                  name: github-client
                  key: clientID
            - name: GITHUB_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: github-client
                  key: clientSecret
        config:
          connectors:
            - type: github
              id: github
              name: GitHub
              config:
                clientID: $GITHUB_CLIENT_ID
                clientSecret: $GITHUB_CLIENT_SECRET
```

### Restricting Access to Specific Organisations

To allow only members of certain GitHub organisations (and optionally specific teams)
to log in, add an `orgs` block to the connector `config`:

```yaml
        config:
          connectors:
            - type: github
              id: github
              name: GitHub
              config:
                clientID: $GITHUB_CLIENT_ID
                clientSecret: $GITHUB_CLIENT_SECRET
                orgs:
                  - name: my-org
                    teams:
                      - developers
                      - admins
                  - name: another-org
```

Refer to the [Dex GitHub connector documentation](https://dexidp.io/docs/connectors/github/)
for the full list of available options, including org and team restrictions.

## Login with OpenShift

When Konflux is deployed on an OpenShift cluster, the operator can automatically
configure a Dex connector that delegates authentication to the cluster's built-in
OAuth server. Users can then log in with any identity provider already configured
in OpenShift (LDAP, HTPasswd, GitHub, etc.).

The behaviour is controlled by `configureLoginWithOpenShift` in `spec.ui.spec.dex.config`:

| Value | Behaviour |
|-------|-----------|
| `true` | OpenShift connector is added when running on OpenShift |
| `false` | OpenShift connector is never added, even on OpenShift |
| *(unset)* | OpenShift connector is added automatically when running on OpenShift |

To explicitly enable OpenShift login:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  ui:
    spec:
      dex:
        config:
          configureLoginWithOpenShift: true
```

When the operator detects OpenShift and this value is unset or `true`, it creates a
`ServiceAccount` and `Secret` in the `konflux-ui` namespace and registers the
cluster's OAuth server as a Dex connector automatically - no additional secrets or
connector configuration is required.

To disable OpenShift login on an OpenShift cluster:

```yaml
        config:
          configureLoginWithOpenShift: false
```

## Generic OIDC Connector

Any OIDC-compliant identity provider (Google, Keycloak, Azure AD, Okta, etc.) can be
added using the `oidc` connector type.

### Example: Google

```yaml
        config:
          connectors:
            - type: oidc
              id: google
              name: Google
              config:
                clientID: $GOOGLE_CLIENT_ID
                clientSecret: $GOOGLE_CLIENT_SECRET
                issuer: https://accounts.google.com
```

Refer to the [Dex OIDC connector documentation](https://dexidp.io/docs/connectors/oidc/)
for the full list of available options.

## LDAP Connector

Konflux supports authenticating users against an LDAP or Active Directory server
through Dex's built-in LDAP connector.

```yaml
        config:
          connectors:
            - type: ldap
              id: ldap
              name: LDAP
              config:
                host: ldap.example.com:636
                bindDN: cn=admin,dc=example,dc=com
                bindPW: $LDAP_BIND_PASSWORD
                userSearch:
                  baseDN: ou=Users,dc=example,dc=com
                  filter: "(objectClass=person)"
                  username: uid
                  idAttr: uid
                  emailAttr: mail
                  nameAttr: cn
                groupSearch:
                  baseDN: ou=Groups,dc=example,dc=com
                  filter: "(objectClass=groupOfNames)"
                  nameAttr: cn
                  userMatchers:
                    - userAttr: DN
                      groupAttr: member
```

Store the bind password in a secret and expose it to the Dex container via an
environment variable:

```bash
kubectl create secret generic ldap-bind \
  --namespace konflux-ui \
  --from-literal=bindPassword="<your-bind-password>"
```

```yaml
      dex:
        dex:
          env:
            - name: LDAP_BIND_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: ldap-bind
                  key: bindPassword
        config:
          connectors:
            - type: ldap
              # ...
```

Refer to the [Dex LDAP connector documentation](https://dexidp.io/docs/connectors/ldap/)
for the complete reference.

## Static Passwords (Local Development Only)

For local development and CI testing, Dex supports a built-in password database. Enable
it with `enablePasswordDB: true` and define users in `staticPasswords`:

```yaml
        config:
          enablePasswordDB: true
          passwordConnector: local
          staticPasswords:
            - email: user1@konflux.dev
              # Generate a bcrypt hash: echo password | htpasswd -BinC 10 admin | cut -d: -f2
              hash: "$2a$10$2b2cU8CPhOTaGrs1HRQuAueS7JTT5ZHsHSzYiFPm1leZck7Mc8T4W" # notsecret
              username: user1
              userID: "7138d2fe-724e-4e86-af8a-db7c4b080e20"
```

{{< alert color="warning" >}}
Static passwords are stored as bcrypt hashes in the Konflux CR, but the CR itself is
visible to anyone with read access to the cluster. <strong>Never use this configuration
in production.</strong> Use an OIDC connector instead.
{{< /alert >}}

## Combining Multiple Connectors

Multiple connectors can be configured simultaneously. Dex presents all of them on its
login page and allows users to choose. The following example enables GitHub, Google, and
OpenShift login together:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  ui:
    spec:
      ingress:
        enabled: true
      dex:
        dex:
          env:
            - name: GITHUB_CLIENT_ID
              valueFrom:
                secretKeyRef:
                  name: github-client
                  key: clientID
            - name: GITHUB_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: github-client
                  key: clientSecret
            - name: GOOGLE_CLIENT_ID
              valueFrom:
                secretKeyRef:
                  name: google-client
                  key: clientID
            - name: GOOGLE_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: google-client
                  key: clientSecret
        config:
          configureLoginWithOpenShift: true
          connectors:
            - type: github
              id: github
              name: GitHub
              config:
                clientID: $GITHUB_CLIENT_ID
                clientSecret: $GITHUB_CLIENT_SECRET
            - type: oidc
              id: google
              name: Google
              config:
                clientID: $GOOGLE_CLIENT_ID
                clientSecret: $GOOGLE_CLIENT_SECRET
                issuer: https://accounts.google.com
```

## Additional Connectors

Dex supports many more upstream providers including Bitbucket Cloud, GitLab, SAML 2.0,
LinkedIn, Microsoft, and more. For the full list of available connectors and their
configuration options, refer to the
[Dex connectors documentation](https://dexidp.io/docs/connectors/).
