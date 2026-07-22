// Package metricsopenshift provides helpers for OpenShift user-workload monitoring (UWM)
// metrics integration tests used by test/go-tests/metricsopenshift.
//
// Operator scrape wiring is documented in operator/docs/component-monitoring.md (deferred
// ServiceMonitor apply and verified TLS). This package implements the test-side waits,
// contract checks, Prometheus queries, and CI log evidence.
package metricsopenshift

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	openshiftMonitoringNamespace = "openshift-monitoring"
	clusterMonitoringConfigName  = "cluster-monitoring-config"
	defaultCanaryQuery           = `up{namespace="dummy-service"} == 1`

	envPromWaitTimeout    = "UWM_PROM_WAIT_TIMEOUT"
	envCanaryWaitTimeout  = "UWM_CANARY_WAIT_TIMEOUT"
	envPollInterval       = "UWM_POLL_INTERVAL"
	envCanaryQuery        = "UWM_CANARY_QUERY"
	envSkipCanary         = "UWM_SKIP_CANARY"
)

// WaitConfig controls UWM readiness polling (env-backed defaults match wait-uwm-ready.sh).
type WaitConfig struct {
	PromWaitTimeout   time.Duration
	CanaryWaitTimeout time.Duration
	PollInterval      time.Duration
	CanaryQuery       string
	SkipCanary        bool
}

// WaitConfigFromEnv returns wait settings from the process environment.
func WaitConfigFromEnv() WaitConfig {
	canaryQuery := os.Getenv(envCanaryQuery)
	if canaryQuery == "" {
		canaryQuery = defaultCanaryQuery
	}

	return WaitConfig{
		PromWaitTimeout:   secondsFromEnv(envPromWaitTimeout, 600),
		CanaryWaitTimeout: secondsFromEnv(envCanaryWaitTimeout, 600),
		PollInterval:      secondsFromEnv(envPollInterval, 15),
		CanaryQuery:       canaryQuery,
		SkipCanary:        skipCanaryFromEnv(),
	}
}

func skipCanaryFromEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envSkipCanary)), "true")
}

func secondsFromEnv(key string, defaultSeconds int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return time.Duration(defaultSeconds) * time.Second
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return time.Duration(defaultSeconds) * time.Second
	}
	return time.Duration(seconds) * time.Second
}

// WaitReady blocks until user-workload monitoring is enabled, the UWM Prometheus deployment
// is ready, and optionally a canary up query succeeds. Called from the metricsopenshift
// BeforeSuite before contract and target specs run.
func WaitReady(ctx context.Context, restConfig *rest.Config, wait WaitConfig) error {
	if restConfig == nil {
		return fmt.Errorf("rest config is required")
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}

	logInfo("Checking %s for enableUserWorkload...", clusterMonitoringConfigName)
	if err := pollUntil(ctx, wait.PollInterval, time.Now().Add(wait.PromWaitTimeout),
		fmt.Sprintf("%s enableUserWorkload: true", clusterMonitoringConfigName),
		func(ctx context.Context) (bool, error) {
			enabled, err := userWorkloadEnabled(ctx, clientset)
			if err != nil {
				if apierrors.IsNotFound(err) {
					return false, nil
				}
				return false, err
			}
			return enabled, nil
		},
	); err != nil {
		return err
	}
	logSuccess("enableUserWorkload: true")

	logInfo("Waiting for prometheus-user-workload pods in %s...", UWMNamespace)
	if err := pollUntil(ctx, wait.PollInterval, time.Now().Add(wait.PromWaitTimeout),
		fmt.Sprintf("prometheus pods ready in %s", UWMNamespace),
		func(ctx context.Context) (bool, error) {
			return prometheusPodsReady(ctx, clientset)
		},
	); err != nil {
		return err
	}
	logSuccess("prometheus-user-workload pod(s) ready")

	if wait.SkipCanary {
		logInfo("Skipping UWM canary wait (UWM_SKIP_CANARY=%s)", os.Getenv(envSkipCanary))
		return nil
	}

	logInfo("Waiting for UWM canary target (%s) to be up...", wait.CanaryQuery)
	if err := pollUntil(ctx, wait.PollInterval, time.Now().Add(wait.CanaryWaitTimeout),
		fmt.Sprintf("UWM canary query %q", wait.CanaryQuery),
		func(ctx context.Context) (bool, error) {
			result, err := QueryPrometheus(ctx, restConfig, wait.CanaryQuery)
			if err != nil {
				return false, nil
			}
			return HasUpSample(result), nil
		},
	); err != nil {
		if result, qerr := QueryPrometheus(ctx, restConfig, wait.CanaryQuery); qerr == nil {
			return fmt.Errorf("%w; last query status=%q samples=%d", err, result.Status, len(result.Data.Result))
		}
		return err
	}
	logSuccess("UWM canary target is up")
	return nil
}

func userWorkloadEnabled(ctx context.Context, clientset kubernetes.Interface) (bool, error) {
	cm, err := clientset.CoreV1().ConfigMaps(openshiftMonitoringNamespace).Get(
		ctx, clusterMonitoringConfigName, metav1.GetOptions{},
	)
	if err != nil {
		return false, err
	}
	return strings.Contains(cm.Data["config.yaml"], "enableUserWorkload: true"), nil
}

func prometheusPodsReady(ctx context.Context, clientset kubernetes.Interface) (bool, error) {
	ready, err := podsReadyWithSelector(ctx, clientset,
		"app.kubernetes.io/name=prometheus,app.kubernetes.io/component=prometheus")
	if err != nil {
		return false, err
	}
	if ready {
		return true, nil
	}
	return podsReadyWithSelector(ctx, clientset, "app.kubernetes.io/name=prometheus")
}

func podsReadyWithSelector(ctx context.Context, clientset kubernetes.Interface, selector string) (bool, error) {
	list, err := clientset.CoreV1().Pods(UWMNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return false, err
	}
	if len(list.Items) == 0 {
		return false, nil
	}
	for _, pod := range list.Items {
		if podReady(&pod) {
			return true, nil
		}
	}
	return false, nil
}

func podReady(pod *corev1.Pod) bool {
	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func pollUntil(
	ctx context.Context,
	interval time.Duration,
	deadline time.Time,
	description string,
	check func(context.Context) (bool, error),
) error {
	if interval <= 0 {
		interval = 15 * time.Second
	}

	started := time.Now()
	for {
		ok, err := check(ctx)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %s", description)
		}
		logWait("%s not ready yet (%s)", description, time.Since(started).Round(time.Second))

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func logInfo(format string, args ...any) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

func logSuccess(msg string) {
	fmt.Printf("[SUCCESS] %s\n", msg)
}

func logWait(format string, args ...any) {
	fmt.Printf("[WAIT] "+format+"\n", args...)
}
