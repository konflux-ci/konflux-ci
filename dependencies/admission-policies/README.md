# CI / local MutatingAdmissionPolicies

In-process replacements for the former Kyverno ClusterPolicies used by
`deploy-deps.sh` on Kind (Kubernetes 1.36+).

| Bundle | Contents |
|--------|----------|
| `policy/` | Reduce Tekton build/EC TaskRun pod CPU/memory requests to `1m`/`1Mi` and set `imagePullPolicy: IfNotPresent` |
| `policy-with-skip-checks-mutation/` | Above + default `skip-checks` to `true` on matched push PipelineRuns |

Selected via `SET_SKIP_CHECKS` in `deploy-deps.sh`. If the MutatingAdmissionPolicy API is missing, deploy skips these policies with a warning.
