# PaC Webhook Chain Debugging

When the failure is **"no pipelinerun found for component …"**, the problem is
almost always in the Pipelines-as-Code (PaC) webhook delivery or processing
chain. The **push event** on the PaC branch is what triggers PaC to create a
PipelineRun (not the `pull_request` event).

## Webhook chain

```
GitHub App webhook → smee.io channel → gosmee client → smee sidecar (:8080) → PaC controller (:8180)
```

## Check each link

### 1. Build-service logs

File: `artifacts/pods/build-service_*.log`

Confirm the PaC Repository was created and the PaC PR was opened:

- `"Created PaC Repository object"` — when the Repository CR was created
- `"Pipelines as Code configuration merge request:"` — the PaC PR URL

Note the **timestamps** — you'll need them to correlate with the gosmee event
log.

### 2. Gosmee relay logs

Location: **raw `job.log`** (not in the structured artifacts).

Search for `"Pod 'gosmee-client-"` to find the gosmee pod's output section.
The gosmee logs list every event relayed from the smee channel:

```
Fri, 20 Mar 2026 13:23:12 UTC INF ... push event replayed to http://localhost:8080, status: 202
```

What to check:

- Were **push events** delivered **after** the PaC Repository was created?
  Correlate gosmee replay timestamps with the build-service log.
- Status `202` = the smee sidecar accepted the event (forwarded to PaC).
- Status `200` = typically heartbeat/keepalive or events PaC doesn't act on.

**Important:** The smee channel is **shared** between AMD64 and ARM64 jobs.
Both gosmee clients receive **all** events from the channel. Push events from
the other arch's PaC branch will also appear. Without the event payload you
cannot distinguish which branch a push event belongs to — count and correlate
timestamps with the build-service log.

### 3. PaC controller logs

File: `artifacts/pods/pipelines-as-code_*.log`

These show whether PaC matched the event to a Repository, read `.tekton/`
from the commit, and attempted to create a PipelineRun. Look for:

- No matching Repository found (informer cache not yet synced after creation)
- GitHub API errors reading the commit tree
- Failures creating the PipelineRun resource

### 4. PaC Repository status

File: `artifacts/repositories.json`

If the `status` field is empty or missing, PaC never successfully processed
any event for that Repository.

## Common root causes

| Cause | Evidence |
|-------|----------|
| Push event arrived before Repository existed in PaC's informer cache | Build-service log shows Repository created *after* the gosmee push event timestamp |
| Push event was from the other arch's branch (not the failing component) | Gosmee shows push events but none align with the failing component's timeline |
| PaC couldn't read `.tekton/` from the commit | PaC controller logs show GitHub API errors |
| Smee sidecar didn't forward to PaC | Gosmee shows 200 (not 202) for push events, or sidecar health checks failing |
| GitHub didn't fire a webhook at all | No push events in the gosmee log after the PaC branch was pushed |
