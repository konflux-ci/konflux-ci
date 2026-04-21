---
title: "Telemetry"
linkTitle: "Telemetry"
weight: 9
description: "How telemetry works in Konflux, how to enable it, what data is collected, and where to find it."
---

Konflux includes an optional telemetry component called **segment-bridge** that
collects anonymized usage data and sends it to [Segment](https://segment.com/)
for downstream analysis in tools such as [Amplitude](https://amplitude.com/).

On **OpenShift**, telemetry is **enabled by default**. On **vanilla
Kubernetes** clusters, telemetry is **disabled by default** and must be
explicitly opted in.

## Enabling telemetry

On OpenShift no action is needed — telemetry is active out of the box. On
vanilla Kubernetes, set `spec.telemetry.enabled` to `true` in your `Konflux`
CR:

```yaml
apiVersion: konflux.konflux-ci.dev/v1alpha1
kind: Konflux
metadata:
  name: konflux
spec:
  telemetry:
    enabled: true
```

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

### Optional overrides

You can override the Segment write key and API endpoint. If omitted the
operator uses the key baked into the image at build time and the default
Segment API (`https://api.segment.io/v1`).

```yaml
spec:
  telemetry:
    enabled: true
    spec:
      # Override the Segment write key (omit to use the build-time default)
      segmentKey: "your-write-key"

      # Override the Segment API base URL — do NOT include "/batch"
      segmentAPIURL: "https://your-segment-proxy.example.com/v1"
```

See the [sample Konflux CR]({{< relref "../examples#konflux-configuration" >}})
for the full configuration reference.

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
- **Cluster identifier** is derived from the OpenShift `ClusterVersion` object.
  On vanilla Kubernetes clusters the `kube-system` namespace UID is used
  instead.
- **No credentials or secrets** are included in telemetry events.
- **No source code, image contents, or build logs** are transmitted.

The only data sent relates to pipeline execution metadata (counts, durations,
outcomes) and component/namespace identifiers in hashed form.

## Accessing telemetry data

Where telemetry data lands depends on the Segment write key used by your
deployment:

- **Build-time default key** — if no `segmentKey` is set in the CR, the
  operator uses the key baked into the image at build time. Data is sent to
  whichever Segment workspace that key belongs to.
- **Custom key** — if you provide your own key via
  `spec.telemetry.spec.segmentKey`, data is routed to your own Segment
  workspace instead.

To view incoming events, log in to [app.segment.com](https://app.segment.com)
with the account that owns the write key and open the source's **Debugger**
tab. Downstream destinations (e.g. Amplitude) are configured within the
Segment workspace.

## Disabling telemetry

To opt out of telemetry (including on OpenShift where it is on by default),
set `enabled` to `false` in your Konflux CR:

```yaml
spec:
  telemetry:
    enabled: false
```

The operator will delete the `KonfluxSegmentBridge` resource and clean up all
segment-bridge resources from the cluster.
