package metricsauth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type portForwardHandle struct {
	stopCh  chan struct{}
	readyCh chan struct{}
}

func startPodPortForward(ctx context.Context, cfg *rest.Config, namespace, podName string, remotePort int32) (*portForwardHandle, int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	localPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, 0, err
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	serverURL, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, 0, err
	}
	serverURL.Path = path

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	forwarder, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, stopCh, readyCh, nil, nil)
	if err != nil {
		return nil, 0, err
	}

	go func() {
		_ = forwarder.ForwardPorts()
	}()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case <-readyCh:
	case <-waitCtx.Done():
		close(stopCh)
		return nil, 0, fmt.Errorf("port-forward to %s pod %s not ready: %w", namespace, podName, waitCtx.Err())
	}

	return &portForwardHandle{stopCh: stopCh, readyCh: readyCh}, localPort, nil
}

// startServicePortForward resolves a ready pod behind svc and port-forwards to it.
// Errors from startPodPortForward are wrapped with the service namespace/name for diagnostics.
func startServicePortForward(ctx context.Context, cfg *rest.Config, svc ServiceRef) (*portForwardHandle, int, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, 0, err
	}

	podName, err := readyPodFromService(ctx, clientset, svc.Namespace, svc.Name, svc.Port)
	if err != nil {
		return nil, 0, err
	}

	pf, port, err := startPodPortForward(ctx, cfg, svc.Namespace, podName, svc.Port)
	if err != nil {
		return nil, 0, fmt.Errorf("service %s/%s: %w", svc.Namespace, svc.Name, err)
	}
	return pf, port, nil
}

// StartPodPortForward listens on 127.0.0.1:0 and forwards to a pod port.
func StartPodPortForward(ctx context.Context, cfg *rest.Config, namespace, podName string, remotePort int32) (*PortForwarder, error) {
	pf, localPort, err := startPodPortForward(ctx, cfg, namespace, podName, remotePort)
	if err != nil {
		return nil, err
	}
	return &PortForwarder{
		stopCh:    pf.stopCh,
		readyCh:   pf.readyCh,
		localPort: localPort,
	}, nil
}
