#!/bin/bash -e


main() {
    echo "Generating error logs" >&2
    generate_logs
    echo "Generated logs sucessfully" >&2
}

generate_logs() {
    local logs_dir="logs"
    local pod_definitions_file="$logs_dir/failed-pods-definitions.yaml"
    local pod_logs_file="$logs_dir/failed-pods-logs.log"
    local event_messages_file="$logs_dir/failed-deployment-event-log.log"

    rm -rf "$logs_dir"
    mkdir -p "$logs_dir"

    local namespaces
    namespaces=$(kubectl get namespaces -o name | xargs -n1 basename)

    for namespace in $namespaces; do
        # Get all 'Warning' type events that occured on the namespace and extract the relevant fields from it as variables.
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
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi


