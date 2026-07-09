package metricsopenshift

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sclient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	konfluxkubernetes "github.com/konflux-ci/konflux-ci/operator/pkg/kubernetes"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/metricsauth"
)

const (
	envDebugLogInterval      = "UWM_DEBUG_LOG_INTERVAL"
	envDebugDirectScrape     = "UWM_DEBUG_DIRECT_SCRAPE"
	envDebugOperatorLogLines = "UWM_DEBUG_OPERATOR_LOG_LINES"
	defaultDebugLogPeriod    = 60 * time.Second
	defaultOperatorLogLines  = 500
)

// UWMPollLogState tracks throttling for progress logs during Eventually polling.
type UWMPollLogState struct {
	PollCount       int
	lastLog         time.Time
	lastFingerprint string
}

// DebugLogIntervalFromEnv returns how often to emit progress logs while waiting for up==1.
// 0 means log only on the first poll and when the result fingerprint changes.
func DebugLogIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envDebugLogInterval))
	if raw == "" {
		return defaultDebugLogPeriod
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return defaultDebugLogPeriod
	}
	return time.Duration(seconds) * time.Second
}

func debugDirectScrapeEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envDebugDirectScrape)), "true")
}

// LogUWMPollProgress writes compact UWM query state to w when polling has not yet succeeded.
func LogUWMPollProgress(
	w io.Writer,
	cfg *rest.Config,
	target metricsauth.Target,
	strictPromQL string,
	result *QueryResult,
	state *UWMPollLogState,
) {
	if w == nil || state == nil {
		return
	}
	state.PollCount++
	fingerprint := FormatUpResult(result)
	if !shouldEmitPollLog(state, fingerprint) {
		state.lastFingerprint = fingerprint
		return
	}

	fmt.Fprintf(w, "[UWM debug] %s poll %d strict (%s): %s\n",
		target.ID, state.PollCount, strictPromQL, fingerprint)

	ctx, cancel := context.WithTimeout(context.Background(), prometheusQueryTimeout)
	defer cancel()
	broadPromQL := BroadUpPromQL(target)
	if broad, err := QueryPrometheus(ctx, cfg, broadPromQL); err != nil {
		fmt.Fprintf(w, "[UWM debug] %s poll %d broad (%s): query error: %v\n",
			target.ID, state.PollCount, broadPromQL, err)
	} else {
		fmt.Fprintf(w, "[UWM debug] %s poll %d broad (%s): %s\n",
			target.ID, state.PollCount, broadPromQL, FormatUpResult(broad))
	}

	state.lastLog = time.Now()
	state.lastFingerprint = fingerprint
}

func shouldEmitPollLog(state *UWMPollLogState, fingerprint string) bool {
	if state.PollCount == 1 {
		return true
	}
	if fingerprint != state.lastFingerprint {
		return true
	}
	interval := DebugLogIntervalFromEnv()
	if interval == 0 {
		return false
	}
	if state.lastLog.IsZero() {
		return true
	}
	return time.Since(state.lastLog) >= interval
}

// DumpTargetDebugOnFailure emits a one-shot cluster and Prometheus snapshot for CI logs.
// peerTargets should include all UWM catalog targets for side-by-side comparison.
func DumpTargetDebugOnFailure(
	ctx context.Context,
	w io.Writer,
	cfg *rest.Config,
	kube client.Client,
	target metricsauth.Target,
	strictPromQL string,
	peerTargets []metricsauth.Target,
) {
	if w == nil {
		return
	}

	fmt.Fprintf(w, "[UWM debug] %s failure snapshot begin\n", target.ID)

	queryCtx, cancel := context.WithTimeout(ctx, prometheusQueryTimeout)
	defer cancel()
	if strict, err := QueryPrometheus(queryCtx, cfg, strictPromQL); err != nil {
		fmt.Fprintf(w, "[UWM debug] %s final strict query error: %v\n", target.ID, err)
	} else {
		fmt.Fprintf(w, "[UWM debug] %s final strict (%s): %s\n", target.ID, strictPromQL, FormatUpResult(strict))
	}

	broadPromQL := BroadUpPromQL(target)
	if broad, err := QueryPrometheus(queryCtx, cfg, broadPromQL); err != nil {
		fmt.Fprintf(w, "[UWM debug] %s final broad query error: %v\n", target.ID, err)
	} else {
		fmt.Fprintf(w, "[UWM debug] %s final broad (%s): %s\n", target.ID, broadPromQL, FormatUpResult(broad))
	}

	targetsCtx, targetsCancel := context.WithTimeout(ctx, prometheusQueryTimeout)
	defer targetsCancel()
	promTargets, promTargetsErr := FetchPrometheusTargets(targetsCtx, cfg)
	if promTargetsErr != nil {
		fmt.Fprintf(w, "[UWM debug] prometheus targets error: %v\n", promTargetsErr)
	} else {
		dumpUWMPeerComparison(ctx, w, cfg, promTargets, peerTargets)
		fmt.Fprintf(w, "[UWM debug] %s prometheus active targets detail:\n%s\n",
			target.ID, FormatTargetsForNamespace(promTargets, target.Namespace))
		fmt.Fprintf(w, "[UWM debug] %s prometheus dropped targets detail:\n%s\n",
			target.ID, FormatDroppedTargetsForNamespace(promTargets, target.Namespace))
	}

	dumpServiceMonitorComparison(ctx, w, kube, target, peerTargets)
	dumpClusterSnapshot(ctx, w, cfg, kube, target)
	dumpPrometheusOperatorLogs(ctx, w, cfg, target.Namespace)

	if debugDirectScrapeEnabled() {
		dumpDirectScrape(ctx, w, cfg, kube, target)
	}

	fmt.Fprintf(w, "[UWM debug] %s failure snapshot end\n", target.ID)
}

func dumpUWMPeerComparison(
	ctx context.Context,
	w io.Writer,
	cfg *rest.Config,
	promTargets *TargetsResult,
	peerTargets []metricsauth.Target,
) {
	if len(peerTargets) == 0 {
		return
	}

	fmt.Fprintf(w, "[UWM debug] UWM peer comparison (targets + up query at failure time):\n")
	queryCtx, cancel := context.WithTimeout(ctx, prometheusQueryTimeout)
	defer cancel()

	for _, peer := range peerTargets {
		activeCount := CountTargetsForNamespace(promTargets, peer.Namespace)
		droppedCount := CountDroppedTargetsForNamespace(promTargets, peer.Namespace)
		upSummary := "query error"
		if result, err := QueryPrometheus(queryCtx, cfg, UpPromQL(peer)); err != nil {
			upSummary = fmt.Sprintf("query error: %v", err)
		} else {
			upSummary = FormatUpResult(result)
		}
		marker := " "
		fmt.Fprintf(w, "[UWM debug] %s %-18s namespace=%-20s active_targets=%d dropped_targets=%d up_strict=%s\n",
			marker, peer.ID, peer.Namespace, activeCount, droppedCount, upSummary)
	}
}

func dumpServiceMonitorComparison(
	ctx context.Context,
	w io.Writer,
	kube client.Client,
	failedTarget metricsauth.Target,
	peerTargets []metricsauth.Target,
) {
	peers := peerTargets
	if len(peers) == 0 {
		peers = []metricsauth.Target{failedTarget}
	}

	fmt.Fprintf(w, "[UWM debug] ServiceMonitor + scrape secret comparison:\n")
	for _, peer := range peers {
		dumpOperandScrapeResources(ctx, w, kube, peer)
	}
}

func dumpOperandScrapeResources(ctx context.Context, w io.Writer, kube client.Client, target metricsauth.Target) {
	dumpNamespaceMonitoringLabels(ctx, w, kube, target)
	dumpServiceMonitorSelectorMatch(ctx, w, kube, target)

	smName := ServiceMonitorName(target)
	sm, smErr := GetServiceMonitor(ctx, kube, target.Namespace, smName)
	if smErr != nil {
		fmt.Fprintf(w, "[UWM debug]   %s SM %s/%s: %v\n", target.ID, target.Namespace, smName, smErr)
	} else {
		fmt.Fprintf(w, "[UWM debug]   %s SM %s: %s\n", target.ID, smName, formatServiceMonitorSummary(sm))
	}

	allSMs, listErr := ListServiceMonitorsInNamespace(ctx, kube, target.Namespace)
	if listErr != nil {
		fmt.Fprintf(w, "[UWM debug]   %s ServiceMonitors in %s: list error: %v\n", target.ID, target.Namespace, listErr)
	} else if len(allSMs) > 1 {
		names := make([]string, 0, len(allSMs))
		for _, sm := range allSMs {
			names = append(names, sm.GetName())
		}
		fmt.Fprintf(w, "[UWM debug]   %s note: %d ServiceMonitors in namespace (expected 1): %v\n",
			target.ID, len(allSMs), names)
	}

	secret := &corev1.Secret{}
	secretKey := client.ObjectKey{Namespace: target.Namespace, Name: konfluxkubernetes.ScrapeTokenSecretName}
	if err := kube.Get(ctx, secretKey, secret); err != nil {
		fmt.Fprintf(w, "[UWM debug]   %s secret %s: %v\n", target.ID, secretKey.Name, err)
	} else {
		fmt.Fprintf(w, "[UWM debug]   %s secret %s: %s\n",
			target.ID, secretKey.Name, formatScrapeSecretSummary(secret))
	}
}

func dumpNamespaceMonitoringLabels(ctx context.Context, w io.Writer, kube client.Client, target metricsauth.Target) {
	ns := &corev1.Namespace{}
	if err := kube.Get(ctx, client.ObjectKey{Name: target.Namespace}, ns); err != nil {
		fmt.Fprintf(w, "[UWM debug]   %s namespace %s: %v\n", target.ID, target.Namespace, err)
		return
	}
	fmt.Fprintf(w, "[UWM debug]   %s namespace %s: metadata=%s labels=%v\n",
		target.ID, target.Namespace, formatNamespaceMetadata(ns), monitoringRelevantLabels(ns.Labels))
}

func dumpServiceMonitorSelectorMatch(ctx context.Context, w io.Writer, kube client.Client, target metricsauth.Target) {
	smName := ServiceMonitorName(target)
	sm, err := GetServiceMonitor(ctx, kube, target.Namespace, smName)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug]   %s service selector check: ServiceMonitor %s/%s: %v\n",
			target.ID, target.Namespace, smName, err)
		return
	}

	selector, err := ServiceMonitorMatchLabels(sm)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug]   %s service selector check: %v\n", target.ID, err)
		return
	}

	svc := &corev1.Service{}
	if err := kube.Get(ctx, client.ObjectKey{Namespace: target.Namespace, Name: target.Service}, svc); err != nil {
		fmt.Fprintf(w, "[UWM debug]   %s service selector check: service %s/%s: %v selector=%v\n",
			target.ID, target.Namespace, target.Service, err, selector)
		return
	}

	matches, mismatches := SelectorMatchReport(svc.Labels, selector)
	if matches {
		fmt.Fprintf(w, "[UWM debug]   %s service selector check: service %s labels match SM selector %v\n",
			target.ID, target.Service, selector)
		return
	}
	fmt.Fprintf(w, "[UWM debug]   %s service selector check: service %s labels=%v SM selector=%v mismatches=%v\n",
		target.ID, target.Service, svc.Labels, selector, mismatches)
}

func formatNamespaceMetadata(ns *corev1.Namespace) string {
	if ns == nil {
		return "nil"
	}
	return fmt.Sprintf(
		"rv=%s created=%s uid=%s",
		ns.ResourceVersion,
		formatTimestamp(ns.CreationTimestamp),
		ns.UID,
	)
}

func monitoringRelevantLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	relevant := make(map[string]string)
	for key, value := range labels {
		if strings.HasPrefix(key, "openshift.io/") || strings.HasPrefix(key, "pod-security") {
			relevant[key] = value
		}
	}
	if len(relevant) == 0 {
		return map[string]string{"<none>": "no openshift.io or pod-security labels"}
	}
	return relevant
}

func formatServiceMonitorSummary(sm *unstructured.Unstructured) string {
	if sm == nil {
		return "nil"
	}
	scheme, bearerSecret, err := ServiceMonitorEndpointScheme(sm)
	if err != nil {
		return fmt.Sprintf("metadata=%s endpoint_error=%v", formatObjectMetadata(sm), err)
	}
	return fmt.Sprintf(
		"metadata=%s scheme=%s bearerTokenSecret=%s labels=%v resync=%q resync_reason=%q",
		formatObjectMetadata(sm), scheme, bearerSecret, sm.GetLabels(),
		ServiceMonitorResyncAt(sm), ServiceMonitorResyncReason(sm),
	)
}

func formatScrapeSecretSummary(secret *corev1.Secret) string {
	if secret == nil {
		return "nil"
	}
	hasToken := len(secret.Data[konfluxkubernetes.ScrapeTokenSecretKey]) > 0
	return fmt.Sprintf(
		"metadata=%s has_token=%t keys=%v",
		formatSecretMetadata(secret), hasToken, secretKeyNames(secret),
	)
}

func formatObjectMetadata(obj *unstructured.Unstructured) string {
	if obj == nil {
		return "nil"
	}
	return fmt.Sprintf(
		"rv=%s created=%s uid=%s",
		obj.GetResourceVersion(),
		formatTimestamp(obj.GetCreationTimestamp()),
		obj.GetUID(),
	)
}

func formatSecretMetadata(secret *corev1.Secret) string {
	if secret == nil {
		return "nil"
	}
	return fmt.Sprintf(
		"rv=%s created=%s uid=%s",
		secret.ResourceVersion,
		formatTimestamp(secret.CreationTimestamp),
		secret.UID,
	)
}

func formatTimestamp(ts metav1.Time) string {
	if ts.IsZero() {
		return "<unknown>"
	}
	age := time.Since(ts.Time).Round(time.Second)
	return fmt.Sprintf("%s age=%s", ts.UTC().Format(time.RFC3339), age)
}

func dumpClusterSnapshot(
	ctx context.Context,
	w io.Writer,
	cfg *rest.Config,
	kube client.Client,
	target metricsauth.Target,
) {
	if ready, err := metricsauth.WaitForServiceEndpointsReady(ctx, kube, target.Namespace, target.Service, target.Port); err != nil {
		fmt.Fprintf(w, "[UWM debug] %s endpoints %s/%s:%d: error: %v\n",
			target.ID, target.Namespace, target.Service, target.Port, err)
	} else {
		fmt.Fprintf(w, "[UWM debug] %s endpoints %s/%s:%d ready=%t\n",
			target.ID, target.Namespace, target.Service, target.Port, ready)
	}

	clientset, err := k8sclient.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] %s kubernetes client: %v\n", target.ID, err)
		return
	}

	list, err := clientset.CoreV1().Pods(target.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "control-plane=controller-manager",
	})
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] %s controller-manager pods: %v\n", target.ID, err)
		return
	}
	if len(list.Items) == 0 {
		fmt.Fprintf(w, "[UWM debug] %s controller-manager pods: none found\n", target.ID)
		return
	}
	for _, pod := range list.Items {
		fmt.Fprintf(w, "[UWM debug] %s pod %s/%s phase=%s ready=%t restarts=%d\n",
			target.ID, pod.Namespace, pod.Name, pod.Status.Phase, podReady(&pod), podRestartCount(&pod))
	}

	deployments, err := clientset.AppsV1().Deployments(target.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "control-plane=controller-manager",
	})
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] %s controller-manager deployments: %v\n", target.ID, err)
		return
	}
	for _, dep := range deployments.Items {
		fmt.Fprintf(w, "[UWM debug] %s deployment %s/%s ready=%d/%d updated=%d available=%d\n",
			target.ID, dep.Namespace, dep.Name,
			dep.Status.ReadyReplicas, desiredReplicas(&dep),
			dep.Status.UpdatedReplicas, dep.Status.AvailableReplicas)
	}
}

func operatorLogLineLimit() int64 {
	raw := strings.TrimSpace(os.Getenv(envDebugOperatorLogLines))
	if raw == "" {
		return defaultOperatorLogLines
	}
	lines, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || lines <= 0 {
		return defaultOperatorLogLines
	}
	return lines
}

func dumpPrometheusOperatorLogs(ctx context.Context, w io.Writer, cfg *rest.Config, namespaceFilter string) {
	clientset, err := k8sclient.NewForConfig(cfg)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] prometheus-operator logs: client error: %v\n", err)
		return
	}

	podName, err := prometheusOperatorPodName(ctx, clientset)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] prometheus-operator logs: %v\n", err)
		return
	}

	logCtx, cancel := context.WithTimeout(ctx, prometheusQueryTimeout)
	defer cancel()
	tailLines := operatorLogLineLimit()
	req := clientset.CoreV1().Pods(UWMNamespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: "prometheus-operator",
		TailLines: &tailLines,
	})
	stream, err := req.Stream(logCtx)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] prometheus-operator logs pod/%s: %v\n", podName, err)
		return
	}
	defer stream.Close()

	body, err := io.ReadAll(stream)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] prometheus-operator logs pod/%s: read error: %v\n", podName, err)
		return
	}

	fmt.Fprintf(w, "[UWM debug] prometheus-operator logs pod/%s (filtered for %q and ServiceMonitor skips):\n",
		podName, namespaceFilter)
	printFilteredOperatorLogLines(w, string(body), namespaceFilter)
}

func prometheusOperatorPodName(ctx context.Context, clientset k8sclient.Interface) (string, error) {
	list, err := clientset.CoreV1().Pods(UWMNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/name=prometheus-operator",
	})
	if err != nil {
		return "", err
	}
	for _, pod := range list.Items {
		if pod.Status.Phase == corev1.PodRunning {
			return pod.Name, nil
		}
	}
	for _, pod := range list.Items {
		if strings.Contains(pod.Name, "prometheus-operator") {
			return pod.Name, nil
		}
	}
	if len(list.Items) == 0 {
		return "", fmt.Errorf("no prometheus-operator pods in %s", UWMNamespace)
	}
	return list.Items[0].Name, nil
}

func printFilteredOperatorLogLines(w io.Writer, raw string, namespaceFilter string) {
	lines := strings.Split(raw, "\n")
	filterTerms := []string{
		namespaceFilter,
		"skipping servicemonitor",
		"Syncing ServiceMonitor",
		"servicemonitor",
		"ServiceMonitor",
	}
	matched := 0
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, term := range filterTerms {
			if term != "" && strings.Contains(lower, strings.ToLower(term)) {
				fmt.Fprintf(w, "[UWM debug]   %s\n", strings.TrimSpace(line))
				matched++
				break
			}
		}
	}
	if matched == 0 {
		fmt.Fprintf(w, "[UWM debug]   (no log lines matched namespace %q or ServiceMonitor events in tail)\n", namespaceFilter)
	}
}

func dumpDirectScrape(ctx context.Context, w io.Writer, cfg *rest.Config, kube client.Client, target metricsauth.Target) {
	token, err := metricsauth.SecretToken(ctx, kube, target.Namespace, target.ScrapeTokenSecret, konfluxkubernetes.ScrapeTokenSecretKey)
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] %s direct scrape: token read error: %v\n", target.ID, err)
		return
	}

	pf, err := metricsauth.StartPortForward(ctx, cfg, metricsauth.ServiceRef{
		Namespace: target.Namespace,
		Name:      target.Service,
		Port:      target.Port,
	})
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] %s direct scrape: port-forward error: %v\n", target.ID, err)
		return
	}
	defer pf.Close()

	localURL := metricsauth.LocalMetricsURL(pf.LocalPort(), target.Path, target.Scheme)
	result, err := metricsauth.ScrapeLocal(ctx, localURL, token, target.Scheme, target.TLSInsecureSkipVerifyForScrape())
	if err != nil {
		fmt.Fprintf(w, "[UWM debug] %s direct scrape: request error: %v\n", target.ID, err)
		return
	}
	snippet := string(result.Body)
	if len(snippet) > 200 {
		snippet = snippet[:200] + "..."
	}
	fmt.Fprintf(w, "[UWM debug] %s direct scrape %s: status=%d body_prefix=%q\n",
		target.ID, localURL, result.StatusCode, snippet)
}

func secretKeyNames(secret *corev1.Secret) []string {
	if secret == nil || len(secret.Data) == 0 {
		return nil
	}
	keys := make([]string, 0, len(secret.Data))
	for key := range secret.Data {
		keys = append(keys, key)
	}
	return keys
}

func podRestartCount(pod *corev1.Pod) int32 {
	if pod == nil {
		return 0
	}
	var restarts int32
	for _, status := range pod.Status.ContainerStatuses {
		restarts += status.RestartCount
	}
	return restarts
}

func desiredReplicas(dep *appsv1.Deployment) int32 {
	if dep == nil {
		return 0
	}
	if dep.Spec.Replicas != nil {
		return *dep.Spec.Replicas
	}
	return 1
}
