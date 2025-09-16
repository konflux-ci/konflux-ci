#!/bin/bash -e


main() {
    echo "Generating error logs" >&2
    generate_logs
    echo "Generated logs successfully" >&2
}

generate_logs() {
    local logs_dir="logs"
    local pod_definitions_file="$logs_dir/failed-pods-definitions.yaml"
    local pod_logs_file="$logs_dir/failed-pods-logs.log"
    local event_messages_file="$logs_dir/failed-deployment-event-log.log"
    local pipelinerun_res_file="$logs_dir/pipelinerun-res.log"
    local taskrun_res_file="$logs_dir/taskrun-res.log"

    rm -rf "$logs_dir"
    mkdir -p "$logs_dir"

    # Collect resource monitoring information
    echo "Collecting resource monitoring information..." >&2

    # Host system resources
    {
        echo "=== HOST SYSTEM RESOURCES ==="
        echo "Date: $(date)"
        echo ""

        echo "--- Host Memory ---"
        free -h 2>&1 || echo "Failed to get memory info"
        echo ""

        echo "--- Host CPU ---"
        lscpu 2>&1 || echo "Failed to get CPU info"
        echo ""

        echo "--- Host Disk Usage ---"
        df -h 2>&1 || echo "Failed to get disk info"
        echo ""

        echo "--- Host Load Average ---"
        uptime 2>&1 || echo "Failed to get load average"
        echo ""

        echo "--- Process Resource Usage (Top 20) ---"
        ps aux --sort=-%mem | head -21 2>&1 || echo "Failed to get process info"
        echo ""
    } > "$logs_dir/system-resources.log"

    # Kubernetes cluster resources
    {
        echo "=== KUBERNETES CLUSTER RESOURCES ==="
        echo "Date: $(date)"
        echo ""

        echo "--- Node Details ---"
        kubectl describe nodes 2>&1 || echo "Failed to describe nodes"
        echo ""


        echo "--- Pending Pods ---"
        kubectl get pods --all-namespaces --field-selector=status.phase=Pending 2>&1 || echo "Failed to get pending pods"
        echo ""

        echo "--- All Pod Status Summary ---"
        kubectl get pods --all-namespaces -o wide 2>&1 || echo "Failed to get pod status"
        echo ""
    } > "$logs_dir/cluster-resources.log"

    # Capture pods matching Kyverno policy labels
    {
        echo "=== PODS MATCHING KYVERNO POLICY LABELS ==="
        echo "Date: $(date)"
        echo ""

        echo "--- Pods with label: pipelines.appstudio.openshift.io/type=build ---"
        kubectl get pods --all-namespaces -l "pipelines.appstudio.openshift.io/type=build" --show-labels -o wide 2>&1 || echo "No pods found with pipelines.appstudio.openshift.io/type=build label"
        echo ""

        echo "--- Pods with label: tekton.dev/task=verify-enterprise-contract ---"
        kubectl get pods --all-namespaces -l "tekton.dev/task=verify-enterprise-contract" --show-labels -o wide 2>&1 || echo "No pods found with tekton.dev/task=verify-enterprise-contract label"
        echo ""

        echo "--- All build pipeline pods (broader search) ---"
        kubectl get pods --all-namespaces -l "tekton.dev/pipelineRun" --show-labels -o wide 2>&1 || echo "No pods found with tekton.dev/pipelineRun label"
        echo ""

        echo "--- All taskrun pods (broader search) ---"
        kubectl get pods --all-namespaces -l "tekton.dev/taskRun" --show-labels -o wide 2>&1 || echo "No pods found with tekton.dev/taskRun label"
        echo ""
    } > "$logs_dir/kyverno-policy-pods.log"

    # Capture detailed definitions for pods matching Kyverno policy
    {
        echo "=== DETAILED DEFINITIONS FOR KYVERNO POLICY PODS ==="
        echo "Date: $(date)"
        echo ""

        echo "--- Pod definitions with label: pipelines.appstudio.openshift.io/type=build ---"
        kubectl get pods --all-namespaces -l "pipelines.appstudio.openshift.io/type=build" -o yaml 2>&1 || echo "No pods found with pipelines.appstudio.openshift.io/type=build label"
        echo ""
        echo "---"
        echo ""

        echo "--- Pod definitions with label: tekton.dev/task=verify-enterprise-contract ---"
        kubectl get pods --all-namespaces -l "tekton.dev/task=verify-enterprise-contract" -o yaml 2>&1 || echo "No pods found with tekton.dev/task=verify-enterprise-contract label"
        echo ""
        echo "---"
        echo ""

        echo "--- Pod definitions with tekton.dev/taskRun label (broader search) ---"
        kubectl get pods --all-namespaces -l "tekton.dev/taskRun" -o yaml 2>&1 || echo "No pods found with tekton.dev/taskRun label"
        echo ""
    } > "$logs_dir/kyverno-policy-pod-definitions.yaml"

    # Docker/containerd resources
    {
        echo "=== CONTAINER RUNTIME RESOURCES ==="
        echo "Date: $(date)"
        echo ""

        echo "--- Docker Container Stats ---"
        timeout 10 docker stats --no-stream --format "table {{.Container}}\t{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}\t{{.NetIO}}\t{{.BlockIO}}" 2>&1 || echo "Failed to get docker stats or docker not available"
        echo ""

        echo "--- Running Containers ---"
        docker ps --format "table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}" 2>&1 || echo "Failed to list containers"
    } > "$logs_dir/container-resources.log"

    local namespaces
    namespaces=$(kubectl get namespaces -o name | xargs -n1 basename)

    for namespace in $namespaces; do
        # Get all 'Warning' type events that occurred on the namespace and extract the relevant fields from it as variables.
        echo -e "----------\nnamespace '$namespace'\n----------"
        local events
        events=$(kubectl get events -n "$namespace" \
                --field-selector type=Warning \
                -o jsonpath='{range .items[*]}{.involvedObject.kind}{" "}{.involvedObject.name}{" "}{.message}{" ("}{.reason}{")\n"}{end}'

        )

        echo "$events" | while read -r kind name message reason; do
            if [ "$kind" == "Pod" ]; then
                local pod_definition
                local pod_logs
                pod_definition=$(kubectl get pod -n "$namespace" "$name" -o yaml 2>&1 || echo "Failed to get pod definition for $name in namespace $namespace")
                pod_logs=$(kubectl logs -n "$namespace" "$name" --all-containers=true 2>&1 || echo "Failed to get pod logs for $name in namespace $namespace")

                printf "%s\n---\n" "$pod_definition" | tee -a "$pod_definitions_file"
                echo "Pod '$name' under namespace '$namespace':" | tee -a "$pod_logs_file" | tee -a "$event_messages_file"
                echo "$kind $name $message $reason" | tee -a "$event_messages_file"
                echo "$pod_logs" | tee -a "$pod_logs_file"
            fi
        done
    done

    kubectl get -A -o yaml pipelineruns | tee "$pipelinerun_res_file"
    kubectl get -A -o yaml taskruns | tee "$taskrun_res_file"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi


