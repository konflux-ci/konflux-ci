package sprayproxy

import "net/http"

const (
	sprayProxyNamespace = "sprayproxy"
	sprayProxyName      = "sprayproxy-route"
	pacNamespace        = "openshift-pipelines"
	pacRouteName        = "pipelines-as-code-controller"
)

type SprayProxyConfig struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}
