# konflux-ci
Integration and release of Konflux-CI

- has requires secret for creating gitops repos
- chains signing and push secret
- build-service github app (global or namespace)
- integration-service github app

## Accessing The UI

Add the following entry to `/etc/hosts`

```bash
127.0.0.1 ui.konflux.dev
```

Open your browser and navigate to: https://ui.konflux.dev:6443/application-pipeline


Since the ingress uses a self signed certificate, you would need to approve it in your browser.
Here is how to do it in chrome: https://superuser.com/a/1786445
