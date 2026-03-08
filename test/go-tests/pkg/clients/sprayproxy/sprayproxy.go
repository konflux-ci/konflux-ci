package sprayproxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	kubeCl "github.com/konflux-ci/konflux-ci/test/go-tests/pkg/clients/kubernetes"
	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/types"
)

func NewSprayProxyConfig(url string, token string) (*SprayProxyConfig, error) {
	return &SprayProxyConfig{
		BaseURL: url,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					// #nosec G402
					InsecureSkipVerify: true,
				},
			},
		},
		Token: token,
	}, nil
}

func (s *SprayProxyConfig) RegisterServer(pacHost string) (string, error) {
	bytesData, err := buildBodyData(pacHost)
	if err != nil {
		return "", err
	}

	result, err := s.sendRequest(http.MethodPost, bytes.NewReader(bytesData))
	if err != nil {
		return "", err
	}
	return result, nil
}

func (s *SprayProxyConfig) UnregisterServer(pacHost string) (string, error) {
	bytesData, err := buildBodyData(pacHost)
	if err != nil {
		return "", err
	}

	result, err := s.sendRequest(http.MethodDelete, bytes.NewReader(bytesData))
	if err != nil {
		return "", err
	}
	return result, nil
}

func (s *SprayProxyConfig) GetServers() (string, error) {
	result, err := s.sendRequest(http.MethodGet, nil)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(result, "Backend urls:"), nil
}

func (s *SprayProxyConfig) sendRequest(httpMethod string, data io.Reader) (string, error) {
	requestURL := s.BaseURL + "/backends"

	req, err := http.NewRequest(httpMethod, requestURL, data)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", s.Token))

	res, err := s.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("failed to access SprayProxy server with status code: %d and body: %s", res.StatusCode, string(body))
	}

	defer res.Body.Close()

	return string(body), err
}

func GetPaCHost() (string, error) {
	k8sClient, err := kubeCl.NewAdminKubernetesClient()
	if err != nil {
		return "", err
	}

	namespaceName := types.NamespacedName{
		Name:      pacRouteName,
		Namespace: pacNamespace,
	}

	route := &routev1.Route{}
	err = k8sClient.KubeRest().Get(context.Background(), namespaceName, route)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("https://%s", route.Spec.Host), nil
}

func buildBodyData(pacHost string) ([]byte, error) {
	data := make(map[string]string)
	data["url"] = pacHost
	bytesData, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return bytesData, nil
}
