/*
Some of the code is copied and refactored from GoHooks library: https://pkg.go.dev/github.com/averageflow/gohooks/v2/gohooks
Original version is available on https://github.com/averageflow/gohooks/blob/v2.2.0/gohooks/GoHook.go

MIT License:
Copyright (c) 2013-2014 Onsi Fakhouri

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*/

package framework

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"k8s.io/klog/v2"
)

// GoWebHook represents the definition of a GoWebHook.
type GoWebHook struct {
	// Data to be sent in the GoWebHook
	Payload GoWebHookPayload
	// The encrypted SHA resulting with the used salt
	ResultingSha string
	// Prepared JSON marshaled data
	PreparedData []byte
	// Choice of signature header to use on sending a GoWebHook
	SignatureHeader string
	// Should validate SSL certificate
	IsSecure bool
	// Preferred HTTP method to send the GoWebHook
	// Please choose only POST, DELETE, PATCH or PUT
	// Any other value will make the send use POST as fallback
	PreferredMethod string
	// Additional HTTP headers to be added to the hook
	AdditionalHeaders map[string]string
}

// GoWebHookPayload represents the data that will be sent in the GoWebHook.
type GoWebHookPayload struct {
	Resource string      `json:"resource"`
	Data     interface{} `json:"data"`
}

const (
	DefaultSignatureHeader = "X-GoWebHooks-Verification"
)

// Create creates a webhook to be sent to another system,
// with a SHA 256 signature based on its contents.
func (hook *GoWebHook) Create(data interface{}, resource, secret string) {
	hook.Payload.Resource = resource
	hook.Payload.Data = data

	preparedHookData, err := json.Marshal(hook.Payload)
	if err != nil {
		klog.Error(err.Error())
	}

	hook.PreparedData = preparedHookData

	h := hmac.New(sha256.New, []byte(secret))

	_, err = h.Write(preparedHookData)
	if err != nil {
		klog.Error(err.Error())
	}

	// Get result and encode as hexadecimal string
	hook.ResultingSha = hex.EncodeToString(h.Sum(nil))
}

// Send sends a GoWebHook to the specified URL, as a UTF-8 JSON payload.
func (hook *GoWebHook) Send(receiverURL string) (*http.Response, error) {
	if hook.SignatureHeader == "" {
		// Use the DefaultSignatureHeader as default if no custom header is specified
		hook.SignatureHeader = DefaultSignatureHeader
	}

	if !hook.IsSecure {
		// By default do not verify SSL certificate validity
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec
		}
	}

	switch hook.PreferredMethod {
	case http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete:
		// Valid Methods, do nothing
	default:
		// By default send GoWebHook using a POST method
		hook.PreferredMethod = http.MethodPost
	}

	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(
		hook.PreferredMethod,
		receiverURL,
		bytes.NewBuffer(hook.PreparedData),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Charset", "utf-8")
	req.Header.Add(DefaultSignatureHeader, hook.ResultingSha)

	// Add user's additional headers
	for i := range hook.AdditionalHeaders {
		req.Header.Add(i, hook.AdditionalHeaders[i])
	}

	req.Close = true

	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	return resp, nil
}
