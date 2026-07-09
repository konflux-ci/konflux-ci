# Monitoring overlays

Operator-owned scrape resources live under `<component>/monitoring/`. Full layout,
scrape models, and migration steps: **[../docs/component-monitoring.md](../docs/component-monitoring.md)**.

After editing overlays, rebuild embedded manifests:

```bash
bash operator/pkg/manifests/process-component.sh <component> <repo-root>
```
