---
title: "Telemetry"
linkTitle: "Telemetry"
weight: 9
description: "How telemetry works in Konflux, how to enable it, what data is collected, and where to find it."
---

Konflux includes an optional telemetry component called **segment-bridge** that
collects anonymized usage data and sends it to [Segment](https://segment.com/)
for downstream analysis in tools such as [Amplitude](https://amplitude.com/).

Telemetry is **disabled by default** and must be explicitly enabled in your
`Konflux` CR.

## Enabling telemetry

Set `spec.telemetry.enabled` to `true` and provide a Segment write key. The
operator does not ship a default key — without one, the CronJob may still run
but no events are uploaded to Segment. There are two ways to provide the key:

- **Inline (`spec.telemetry.spec.segmentKey`)** — the write key is stored
  directly in the `Konflux` CR. This is the simplest option and is well
  suited to local or self-deployed Konflux instances where the key doesn't
  need to be managed by external secret tooling.
- **Secret reference (`spec.telemetry.spec.segmentKeySecretRef`)** — the
  write key is read from a Secret key at reconcile time instead of being
  stored in the CR. This is intended for staging/production environments
  where the key is Vault-backed (or otherwise injected/rotated by external
  tooling) and should not be persisted in the CR itself.

When both are set, `segmentKey` takes precedence over `segmentKeySecretRef` —
in that case `segmentKeySecretRef` is ignored entirely (it is not even
resolved), so a stale or invalid Secret reference has no effect as long as
`segmentKey` is set.

### Inline write key

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  telemetry:
    enabled: true
    spec:
      segmentKey: "your-write-key"
```

### Secret-referenced write key

Create the Secret in the `segment-bridge` namespace ahead of time (e.g. via
Vault injection or another external mechanism):

```bash
kubectl create secret generic vault-segment-key \
  --namespace segment-bridge \
  --from-literal=writeKey="your-write-key"
```

Then reference it from the `Konflux` CR instead of setting `segmentKey`:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  telemetry:
    enabled: true
    spec:
      segmentKeySecretRef:
        name: vault-segment-key
        key: writeKey
        # optional: true   # set to true to allow a missing Secret/key without failing reconciliation
```

The referenced Secret follows the standard Kubernetes `SecretKeySelector`
shape (`name`, `key`, and an optional `optional` flag). This ref is only
resolved when `segmentKey` is unset (see above). If the Secret or key
doesn't exist and `optional` is not set to `true`, the operator surfaces a
`False` `Ready` condition on the `KonfluxSegmentBridge` CR with reason
`SecretCreationFailed` until the Secret becomes available.

Apply the change:

```bash
kubectl apply -f <your-konflux-cr>.yaml
```

When telemetry is enabled the operator creates a `KonfluxSegmentBridge` child
resource and deploys the following into the `segment-bridge` namespace:

- A **CronJob** (`segment-bridge`) that runs hourly
- A **Secret** (`segment-bridge-config`) containing the Segment write key,
  batch API URL, and Tekton Results API address
- A **ServiceAccount**, **ClusterRole**, and **ClusterRoleBinding** granting
  read access to PipelineRuns, Components, Namespaces, and Tekton Results

Disabling telemetry (setting `enabled: false`) causes the operator to clean
up all of these resources automatically.

### Segment endpoint

By default, events are sent to Segment's public API at
`https://api.segment.io/v1` (the operator appends `/batch` for the upload
endpoint). To route through a proxy or alternate host, set `segmentAPIURL` to
the base URL only — do not include `/batch`:

```yaml
spec:
  telemetry:
    enabled: true
    spec:
      segmentKey: "your-write-key"
      segmentAPIURL: "https://your-segment-proxy.example.com/v1"
```

See the [sample Konflux CR]({{< relref "../examples#konflux-configuration" >}})
for the full configuration reference.

### Tuning TEKTON_LIMIT

The `segment-bridge` CronJob queries Tekton Results for PipelineRuns that have
already been pruned from the cluster, using a 4-hour lookback window for
resilience. `TEKTON_LIMIT` caps how many records are fetched per run. The
operator defaults to **1000** (upstream `segment-bridge` default is 100),
sized to cover the observed average production throughput of ~214
PipelineRuns/hour (~856 records across the 4-hour window).

**Quota usage:** each PipelineRun emits at least 4 KPI events, and the
4-hour lookback means each event can be sent up to 4 times before Segment's
server-side deduplication discards the repeats — up to **~16 Segment API
calls per PipelineRun**. Every call counts against the Segment API quota,
including duplicates that get deduplicated downstream, so raising
`tektonLimit` (or enabling telemetry on more clusters) directly increases
quota consumption.

If your cluster has higher PipelineRun throughput, size `tektonLimit` using:

```
tektonLimit >= PipelineRuns_per_hour * 4
```

Override via `spec.telemetry.spec.tektonLimit`:

```yaml
spec:
  telemetry:
    enabled: true
    spec:
      tektonLimit: 2000
```

### Customizing the CronJob container

You can override the resource requests/limits and add environment variables
on the `segment-bridge` CronJob container via `spec.telemetry.spec.cronJob`,
following the same `ContainerSpec` pattern used by other operator-managed
components:

```yaml
spec:
  telemetry:
    enabled: true
    spec:
      cronJob:
        resources:
          limits:
            cpu: 500m
            memory: 512Mi
          requests:
            cpu: 100m
            memory: 128Mi
        env:
          - name: HTTP_PROXY
            value: "http://proxy.example.com:3128"
```

`env` entries are applied as pod-level overrides on top of the container's
existing environment — they do **not** replace the `envFrom` reference to the
`segment-bridge-config` Secret, which continues to supply `SEGMENT_WRITE_KEY`,
`SEGMENT_BATCH_API`, `TEKTON_RESULTS_API_ADDR`, and `TEKTON_LIMIT`.

{{% alert color="warning" %}}
An `env` entry whose `name` matches one of `SEGMENT_WRITE_KEY`,
`SEGMENT_BATCH_API`, `TEKTON_RESULTS_API_ADDR`, or `TEKTON_LIMIT` will take
precedence over the Secret-sourced value, per standard Kubernetes container
env-vs-envFrom precedence rules. Avoid reusing these names unless you intend
to override the corresponding Secret value.
{{% /alert %}}

## What data is collected

The segment-bridge CronJob reads data from the cluster and from
[Tekton Results](https://github.com/tektoncd/results) (for PipelineRuns that
have been pruned) and produces Segment events. The following events are
currently emitted:

| Event name | Category |
|------------|----------|
| `PipelineRun Started` | Pipeline activity |
| `PipelineRun Completed` | Pipeline activity |
| `Component Created` | Component lifecycle |
| `Namespace Created` | Namespace lifecycle |
| `Operator Deployment Started` | Operator lifecycle |
| `Operator Deployment Completed` | Operator lifecycle |
| `Operator Removal Started` | Operator lifecycle |
| `Segment Bridge Heartbeat` | Health / liveness |

{{% alert color="info" %}}
Event definitions and properties are maintained in the
<a href="https://github.com/konflux-ci/segment-bridge">segment-bridge</a>
repository (see <code>scripts/tekton-to-segment.sh</code>). The list above
reflects the events currently visible in production.
{{% /alert %}}

## Data flow

**Kubernetes API / Tekton Results → segment-bridge CronJob (hourly) → Segment HTTP API → Amplitude (or other analytics)**

1. The CronJob queries **PipelineRuns**, **Components**, and **Namespaces**
   from the Kubernetes API and **Tekton Results** for historical records.
2. Events are batched into ~500 KB chunks and POSTed to the **Segment Batch
   API** (`SEGMENT_BATCH_API`).
3. Each event carries a deterministic `messageId` so Segment deduplicates
   events that are sent more than once (the CronJob uses a 4-hour lookback
   window for resilience).
4. From Segment, data flows to downstream destinations (e.g. **Amplitude**)
   configured in the Segment workspace. The operator does not manage downstream
   routing — that is handled entirely within Segment.

## Privacy model

Segment-bridge is designed to avoid collecting personally identifiable
information (PII):

- **User and namespace names are hashed.** A one-way hash of the name is
  combined with a **cluster identifier** to produce an opaque, per-cluster
  unique ID. The original names are never sent to Segment.
- **Cluster identifier** is published by the operator in the `konflux-public-info`
  ConfigMap. When no cluster-wide ID is available, the `kube-system` namespace
  UID is used as a fallback.
- **No credentials or secrets** are included in telemetry events.
- **No source code, image contents, or build logs** are transmitted.

The only data sent relates to pipeline execution metadata (counts, durations,
outcomes) and component/namespace identifiers in hashed form.

## Accessing telemetry data

Events are routed to the Segment workspace that owns the write key configured
via `spec.telemetry.spec.segmentKey` or `spec.telemetry.spec.segmentKeySecretRef`.

To view incoming events, log in to [app.segment.com](https://app.segment.com)
with the account that owns the write key and open the source's **Debugger**
tab. Downstream destinations (e.g. Amplitude) are configured within the
Segment workspace.

## Disabling telemetry

To opt out of telemetry, set `enabled` to `false` in your Konflux CR:

```yaml
spec:
  telemetry:
    enabled: false
```

The operator will delete the `KonfluxSegmentBridge` resource and clean up all
segment-bridge resources from the cluster.
