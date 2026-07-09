package metricsopenshift

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestWaitConfigFromEnvDefaults(t *testing.T) {
	t.Setenv(envPromWaitTimeout, "")
	t.Setenv(envCanaryWaitTimeout, "")
	t.Setenv(envPollInterval, "")
	t.Setenv(envCanaryQuery, "")
	t.Setenv(envSkipCanary, "")

	cfg := WaitConfigFromEnv()
	assert.Equal(t, 600*time.Second, cfg.PromWaitTimeout)
	assert.Equal(t, 600*time.Second, cfg.CanaryWaitTimeout)
	assert.Equal(t, 15*time.Second, cfg.PollInterval)
	assert.Equal(t, defaultCanaryQuery, cfg.CanaryQuery)
	assert.False(t, cfg.SkipCanary)
}

func TestWaitConfigFromEnvSkipCanary(t *testing.T) {
	t.Setenv(envSkipCanary, "true")
	cfg := WaitConfigFromEnv()
	assert.True(t, cfg.SkipCanary)
}

func TestWaitConfigFromEnvUnsetCanaryUsesDefault(t *testing.T) {
	t.Setenv(envCanaryQuery, "")
	t.Setenv(envSkipCanary, "")
	cfg := WaitConfigFromEnv()
	assert.Equal(t, defaultCanaryQuery, cfg.CanaryQuery)
	assert.False(t, cfg.SkipCanary)
}

func TestPodReady(t *testing.T) {
	assert.False(t, podReady(nil))
	assert.False(t, podReady(&corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodPending}}))
	assert.True(t, podReady(&corev1.Pod{
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}))
}

func TestUserWorkloadConfigYAMLMarker(t *testing.T) {
	assert.True(t, strings.Contains("enableUserWorkload: true\n", "enableUserWorkload: true"))
	assert.False(t, strings.Contains("enableUserWorkload: false\n", "enableUserWorkload: true"))
}
