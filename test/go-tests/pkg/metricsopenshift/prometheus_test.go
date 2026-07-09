package metricsopenshift

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestHasUpSample(t *testing.T) {
	t.Parallel()

	assert.False(t, HasUpSample(nil))

	assert.False(t, HasUpSample(&QueryResult{}))
	assert.False(t, HasUpSample(&QueryResult{
		Status: "success",
		Data: struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		}{
			Result: []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			}{
				{Metric: map[string]string{"namespace": "build-service"}, Value: []any{1, "0"}},
			},
		},
	}))

	assert.True(t, HasUpSample(&QueryResult{
		Status: "success",
		Data: struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		}{
			Result: []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			}{
				{
					Metric: map[string]string{
						"namespace": "build-service",
						"service":   "build-service-controller-manager-metrics-service",
					},
					Value: []any{1, "1"},
				},
			},
		},
	}))
}

func TestFormatUpResult(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "no result", FormatUpResult(nil))
	assert.Equal(t, "no series", FormatUpResult(&QueryResult{Status: "success"}))
	assert.Contains(t, FormatUpResult(&QueryResult{
		Status: "success",
		Data: struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		}{
			Result: []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			}{
				{Metric: map[string]string{"service": "svc"}, Value: []any{1, "0"}},
			},
		},
	}), "value=0")
}

func TestCountTargetsForNamespace(t *testing.T) {
	t.Parallel()

	result := &TargetsResult{
		Status: "success",
		Data: struct {
			ActiveTargets  []PrometheusTarget `json:"activeTargets"`
			DroppedTargets []PrometheusTarget `json:"droppedTargets"`
		}{
			ActiveTargets: []PrometheusTarget{
				{Labels: map[string]string{"namespace": "build-service"}},
				{Labels: map[string]string{"namespace": "image-controller"}},
				{DiscoveredLabels: map[string]string{"kubernetes_namespace": "build-service"}},
			},
			DroppedTargets: []PrometheusTarget{
				{Labels: map[string]string{"namespace": "build-service"}, LastError: "401 Unauthorized"},
			},
		},
	}
	assert.Equal(t, 2, CountTargetsForNamespace(result, "build-service"))
	assert.Equal(t, 1, CountTargetsForNamespace(result, "image-controller"))
	assert.Equal(t, 0, CountTargetsForNamespace(result, "konflux-operator"))
	assert.Equal(t, 1, CountDroppedTargetsForNamespace(result, "build-service"))
	assert.Equal(t, 0, CountDroppedTargetsForNamespace(result, "image-controller"))
}

func TestFormatTargetsForNamespace(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "no active targets result", FormatTargetsForNamespace(nil, "build-service"))
	assert.Contains(t, FormatTargetsForNamespace(&TargetsResult{
		Status: "success",
		Data: struct {
			ActiveTargets  []PrometheusTarget `json:"activeTargets"`
			DroppedTargets []PrometheusTarget `json:"droppedTargets"`
		}{
			ActiveTargets: []PrometheusTarget{
				{
					Labels:     map[string]string{"namespace": "build-service", "service": "metrics"},
					Health:     "down",
					LastError:  "401 Unauthorized",
					ScrapeURL:  "https://example/metrics",
					LastScrape: "2026-07-09T00:00:00Z",
				},
			},
		},
	}, "build-service"), "401 Unauthorized")
}

func TestFormatDroppedTargetsForNamespace(t *testing.T) {
	t.Parallel()

	assert.Contains(t, FormatDroppedTargetsForNamespace(&TargetsResult{
		Status: "success",
		Data: struct {
			ActiveTargets  []PrometheusTarget `json:"activeTargets"`
			DroppedTargets []PrometheusTarget `json:"droppedTargets"`
		}{
			DroppedTargets: []PrometheusTarget{
				{
					Labels:    map[string]string{"namespace": "build-service", "service": "metrics"},
					LastError: "server returned HTTP status 403 Forbidden",
				},
			},
		},
	}, "build-service"), "403 Forbidden")
	assert.Equal(t, `no dropped targets in namespace "build-service"`, FormatDroppedTargetsForNamespace(&TargetsResult{
		Status: "success",
	}, "build-service"))
}

func TestSelectorMatchReport(t *testing.T) {
	t.Parallel()

	matches, mismatches := SelectorMatchReport(map[string]string{
		"control-plane": "controller-manager",
		"app":           "build-service",
	}, map[string]string{
		"control-plane": "controller-manager",
	})
	assert.True(t, matches)
	assert.Empty(t, mismatches)

	matches, mismatches = SelectorMatchReport(map[string]string{
		"control-plane": "controller-manager",
	}, map[string]string{
		"control-plane": "controller-manager",
		"app":           "build-service",
	})
	assert.False(t, matches)
	assert.Contains(t, mismatches[0], `service missing label "app"`)
}

func TestServiceMonitorMatchLabels(t *testing.T) {
	t.Parallel()

	sm := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"selector": map[string]any{
				"matchLabels": map[string]any{
					"control-plane": "controller-manager",
				},
			},
		},
	}}
	sm.SetNamespace("build-service")
	sm.SetName("build-service")

	labels, err := ServiceMonitorMatchLabels(sm)
	assert.NoError(t, err)
	assert.Equal(t, map[string]string{"control-plane": "controller-manager"}, labels)
}

func TestShouldEmitPollLog(t *testing.T) {
	t.Parallel()

	state := &UWMPollLogState{}
	assert.True(t, shouldEmitPollLog(state, "a"))

	state.PollCount = 2
	state.lastFingerprint = "a"
	state.lastLog = time.Now()
	assert.False(t, shouldEmitPollLog(state, "a"))
	assert.True(t, shouldEmitPollLog(state, "b"))
}
