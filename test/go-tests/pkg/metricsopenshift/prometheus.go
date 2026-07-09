package metricsopenshift

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

const prometheusQueryTimeout = 30 * time.Second

var serviceMonitorGVR = schema.GroupVersionResource{
	Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors",
}

// QueryResult is a subset of the Prometheus HTTP API query response.
type QueryResult struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []any             `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// QueryPrometheus runs an instant query against UWM Prometheus via port-forward to the prometheus pod.
func QueryPrometheus(ctx context.Context, cfg *rest.Config, promql string) (*QueryResult, error) {
	body, err := prometheusHTTPGet(ctx, cfg, "/api/v1/query", url.Values{"query": {promql}})
	if err != nil {
		return nil, err
	}
	var result QueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus query status %q", result.Status)
	}
	return &result, nil
}

// TargetsResult is a subset of the Prometheus targets API response.
type TargetsResult struct {
	Status string `json:"status"`
	Data   struct {
		ActiveTargets  []PrometheusTarget `json:"activeTargets"`
		DroppedTargets []PrometheusTarget `json:"droppedTargets"`
	} `json:"data"`
}

// PrometheusTarget holds scrape target state from /api/v1/targets.
type PrometheusTarget struct {
	DiscoveredLabels map[string]string `json:"discoveredLabels"`
	Labels           map[string]string `json:"labels"`
	ScrapeURL        string            `json:"scrapeUrl"`
	LastError        string            `json:"lastError"`
	Health           string            `json:"health"`
	LastScrape       string            `json:"lastScrape"`
}

// FetchPrometheusTargets returns active scrape targets from UWM Prometheus.
func FetchPrometheusTargets(ctx context.Context, cfg *rest.Config) (*TargetsResult, error) {
	body, err := prometheusHTTPGet(ctx, cfg, "/api/v1/targets", url.Values{"state": {"any"}})
	if err != nil {
		return nil, err
	}
	var result TargetsResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.Status != "success" {
		return nil, fmt.Errorf("prometheus targets status %q", result.Status)
	}
	return &result, nil
}

// FormatUpResult summarizes an instant query result for CI logs.
func FormatUpResult(result *QueryResult) string {
	if result == nil {
		return "no result"
	}
	if len(result.Data.Result) == 0 {
		return "no series"
	}

	lines := make([]string, 0, len(result.Data.Result))
	for _, sample := range result.Data.Result {
		value := "<missing>"
		if len(sample.Value) >= 2 {
			value = fmt.Sprint(sample.Value[1])
		}
		lines = append(lines, fmt.Sprintf("labels=%v value=%s", sample.Metric, value))
	}
	sort.Strings(lines)
	return fmt.Sprintf("%d sample(s): %s", len(lines), strings.Join(lines, "; "))
}

// CountTargetsForNamespace returns how many active Prometheus targets belong to a namespace.
func CountTargetsForNamespace(result *TargetsResult, namespace string) int {
	return countTargetsForNamespace(result, namespace, func(r *TargetsResult) []PrometheusTarget {
		return r.Data.ActiveTargets
	})
}

// CountDroppedTargetsForNamespace returns how many dropped Prometheus targets belong to a namespace.
func CountDroppedTargetsForNamespace(result *TargetsResult, namespace string) int {
	return countTargetsForNamespace(result, namespace, func(r *TargetsResult) []PrometheusTarget {
		return r.Data.DroppedTargets
	})
}

func countTargetsForNamespace(
	result *TargetsResult,
	namespace string,
	targets func(*TargetsResult) []PrometheusTarget,
) int {
	if result == nil {
		return 0
	}
	count := 0
	for _, target := range targets(result) {
		if targetNamespace(target) == namespace {
			count++
		}
	}
	return count
}

// ListServiceMonitorsInNamespace returns ServiceMonitors in a namespace.
func ListServiceMonitorsInNamespace(ctx context.Context, c client.Reader, namespace string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   serviceMonitorGVR.Group,
		Version: serviceMonitorGVR.Version,
		Kind:    "ServiceMonitorList",
	})
	if err := c.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	out := make([]unstructured.Unstructured, 0, len(list.Items))
	for _, item := range list.Items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].GetName() < out[j].GetName()
	})
	return out, nil
}

// FormatTargetsForNamespace summarizes active Prometheus targets that match a namespace.
func FormatTargetsForNamespace(result *TargetsResult, namespace string) string {
	return formatTargetsForNamespace(result, namespace, func(r *TargetsResult) []PrometheusTarget {
		return r.Data.ActiveTargets
	}, "active")
}

// FormatDroppedTargetsForNamespace summarizes dropped Prometheus targets that match a namespace.
func FormatDroppedTargetsForNamespace(result *TargetsResult, namespace string) string {
	return formatTargetsForNamespace(result, namespace, func(r *TargetsResult) []PrometheusTarget {
		return r.Data.DroppedTargets
	}, "dropped")
}

func formatTargetsForNamespace(
	result *TargetsResult,
	namespace string,
	targets func(*TargetsResult) []PrometheusTarget,
	kind string,
) string {
	if result == nil {
		return fmt.Sprintf("no %s targets result", kind)
	}
	var matches []PrometheusTarget
	for _, target := range targets(result) {
		if targetNamespace(target) == namespace {
			matches = append(matches, target)
		}
	}
	if len(matches) == 0 {
		return fmt.Sprintf("no %s targets in namespace %q", kind, namespace)
	}

	lines := make([]string, 0, len(matches))
	for _, target := range matches {
		lines = append(lines, formatPrometheusTargetLine(target))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func formatPrometheusTargetLine(target PrometheusTarget) string {
	service := targetLabel(target, "service")
	if service == "" {
		service = targetLabel(target, "service_name")
	}
	lastError := target.LastError
	if lastError == "" {
		lastError = "<none>"
	}
	return fmt.Sprintf(
		"health=%s service=%s scrapeUrl=%s lastScrape=%s lastError=%s labels=%v discovered=%v",
		target.Health, service, target.ScrapeURL, target.LastScrape, lastError, target.Labels, target.DiscoveredLabels,
	)
}

// ServiceMonitorMatchLabels returns spec.selector.matchLabels from a ServiceMonitor.
func ServiceMonitorMatchLabels(sm *unstructured.Unstructured) (map[string]string, error) {
	if sm == nil {
		return nil, fmt.Errorf("serviceMonitor is nil")
	}
	labels, found, err := unstructured.NestedStringMap(sm.Object, "spec", "selector", "matchLabels")
	if err != nil {
		return nil, err
	}
	if !found || len(labels) == 0 {
		return nil, fmt.Errorf("servicemonitor %s/%s has no spec.selector.matchLabels", sm.GetNamespace(), sm.GetName())
	}
	return labels, nil
}

// SelectorMatchReport compares service labels against a ServiceMonitor selector.
func SelectorMatchReport(serviceLabels, selector map[string]string) (matches bool, mismatches []string) {
	if len(selector) == 0 {
		return false, []string{"empty ServiceMonitor selector"}
	}
	mismatches = make([]string, 0)
	for key, want := range selector {
		got, ok := serviceLabels[key]
		if !ok {
			mismatches = append(mismatches, fmt.Sprintf("service missing label %q (selector wants %q)", key, want))
			continue
		}
		if got != want {
			mismatches = append(mismatches, fmt.Sprintf("label %q=%q selector wants %q", key, got, want))
		}
	}
	sort.Strings(mismatches)
	return len(mismatches) == 0, mismatches
}

func targetNamespace(target PrometheusTarget) string {
	if ns := targetLabel(target, "namespace"); ns != "" {
		return ns
	}
	return targetLabel(target, "kubernetes_namespace")
}

func targetLabel(target PrometheusTarget, key string) string {
	if target.Labels != nil {
		if value := target.Labels[key]; value != "" {
			return value
		}
	}
	if target.DiscoveredLabels != nil {
		return target.DiscoveredLabels[key]
	}
	return ""
}

func prometheusHTTPGet(ctx context.Context, cfg *rest.Config, path string, query url.Values) ([]byte, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	podName, err := prometheusPodName(ctx, clientset)
	if err != nil {
		return nil, err
	}

	pf, err := metricsauth.StartPodPortForward(ctx, cfg, UWMNamespace, podName, 9090)
	if err != nil {
		return nil, err
	}
	defer pf.Close()

	requestURL := fmt.Sprintf("http://127.0.0.1:%d%s", pf.LocalPort(), path)
	if encoded := query.Encode(); encoded != "" {
		requestURL += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: prometheusQueryTimeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "..."
		}
		return nil, fmt.Errorf("prometheus HTTP GET %s: %d: %s", path, resp.StatusCode, snippet)
	}
	return body, nil
}

// HasUpSample reports whether the query result contains at least one sample with value 1.
func HasUpSample(result *QueryResult) bool {
	if result == nil {
		return false
	}
	for _, sample := range result.Data.Result {
		if len(sample.Value) < 2 {
			continue
		}
		if fmt.Sprint(sample.Value[1]) == "1" {
			return true
		}
	}
	return false
}

func prometheusPodName(ctx context.Context, clientset kubernetes.Interface) (string, error) {
	list, err := clientset.CoreV1().Pods(UWMNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=prometheus",
	})
	if err != nil {
		return "", err
	}
	for _, pod := range list.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("no prometheus pods in %s", UWMNamespace)
	}
	return "", fmt.Errorf(
		"no running prometheus pods in %s (%d pods in non-Running state)",
		UWMNamespace,
		len(list.Items),
	)
}

// GetServiceMonitor fetches a ServiceMonitor as unstructured.
func GetServiceMonitor(ctx context.Context, c client.Reader, namespace, name string) (*unstructured.Unstructured, error) {
	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   serviceMonitorGVR.Group,
		Version: serviceMonitorGVR.Version,
		Kind:    "ServiceMonitor",
	})
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, sm); err != nil {
		return nil, err
	}
	return sm, nil
}

// ServiceMonitorEndpointScheme returns scheme and bearerTokenSecret name from the first endpoint.
func ServiceMonitorEndpointScheme(sm *unstructured.Unstructured) (scheme, bearerSecret string, err error) {
	if sm == nil {
		return "", "", fmt.Errorf("serviceMonitor is nil")
	}
	endpoints, found, err := unstructured.NestedSlice(sm.Object, "spec", "endpoints")
	if err != nil {
		return "", "", err
	}
	if !found || len(endpoints) == 0 {
		return "", "", fmt.Errorf("servicemonitor %s/%s has no endpoints", sm.GetNamespace(), sm.GetName())
	}
	ep, ok := endpoints[0].(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("unexpected endpoint shape in %s/%s", sm.GetNamespace(), sm.GetName())
	}
	scheme, _, _ = unstructured.NestedString(ep, "scheme")
	bearerSecret, _, _ = unstructured.NestedString(ep, "bearerTokenSecret", "name")
	return scheme, bearerSecret, nil
}
