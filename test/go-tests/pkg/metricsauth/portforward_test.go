package metricsauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/rest"
)

func TestStartServicePortForwardWrapsPodPortForwardError(t *testing.T) {
	const (
		namespace = "build-service"
		svcName   = "build-service-controller-manager-metrics-service"
		podName   = "metrics-pod-0"
		port      = int32(8443)
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/endpointslices"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"items": [{
					"metadata": {
						"name": "metrics-endpoints",
						"namespace": "` + namespace + `",
						"labels": {"kubernetes.io/service-name": "` + svcName + `"}
					},
					"ports": [{"name": "https", "port": 8443, "protocol": "TCP"}],
					"endpoints": [{
						"conditions": {"ready": true},
						"targetRef": {"kind": "Pod", "name": "` + podName + `"}
					}]
				}]
			}`))
		default:
			http.Error(w, "port-forward unavailable", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	cfg := &rest.Config{
		Host:    server.URL,
		Timeout: time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := startServicePortForward(ctx, cfg, ServiceRef{
		Namespace: namespace,
		Name:      svcName,
		Port:      port,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service "+namespace+"/"+svcName)
}
