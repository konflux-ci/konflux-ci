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

func startServicePortForward(ctx context.Context, cfg *rest.Config, svc ServiceRef) (*portForwardHandle, int, error) {
	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, 0, err
	}

	podName, err := readyPodFromService(ctx, clientset, svc.Namespace, svc.Name, svc.Port)
	if err != nil {
		return nil, 0, err
	}

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

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", svc.Namespace, podName)
	serverURL, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, 0, err
	}
	serverURL.Path = path

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, http.MethodPost, serverURL)
	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	forwarder, err := portforward.New(dialer, []string{fmt.Sprintf("%d:%d", localPort, svc.Port)}, stopCh, readyCh, nil, nil)
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
		return nil, 0, fmt.Errorf("port-forward to %s/%s pod %s not ready: %w", svc.Namespace, svc.Name, podName, waitCtx.Err())
	}

	return &portForwardHandle{stopCh: stopCh, readyCh: readyCh}, localPort, nil
}
