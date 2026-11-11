/*
Copyright 2025 Konflux CI.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("generate-proxy-config.sh", func() {
	var (
		scriptDir string
		templates string
		snippets  string
		staticDir string
		serviceCA string
	)

	BeforeEach(func() {
		scriptDir = proxyConfigScriptDir()
		root := GinkgoT().TempDir()
		templates = filepath.Join(root, "templates")
		snippets = filepath.Join(root, "snippets")
		staticDir = filepath.Join(root, "static")
		serviceCA = filepath.Join(root, "missing-service-ca.crt")
		Expect(os.MkdirAll(templates, 0o755)).To(Succeed())
		Expect(os.MkdirAll(snippets, 0o755)).To(Succeed())
		Expect(os.MkdirAll(staticDir, 0o755)).To(Succeed())
		for _, name := range []string{"tekton-results.caddy", "kite.caddy", "kubearchive.caddy", "watson.caddy"} {
			source, err := os.ReadFile(filepath.Join(scriptDir, name))
			Expect(err).NotTo(HaveOccurred())
			Expect(os.WriteFile(filepath.Join(templates, name), source, 0o644)).To(Succeed())
		}
	})

	It("replaces accepted hostnames in the Caddy templates", func() {
		By("running the script with accepted hostnames")
		out, err := runProxyConfigScript(scriptDir, map[string]string{
			"TEMPLATES_DIR":           templates,
			"SNIPPETS_DIR":            snippets,
			"SERVICE_CA_PATH":         serviceCA,
			"STATIC_DIR":              staticDir,
			"TEKTON_RESULTS_HOSTNAME": "tekton-results.example.svc.",
			"KITE_ENABLED":            "true",
			"KITE_HOSTNAME":           "kite.example.svc:8443",
			"KUBEARCHIVE_ENABLED":     "true",
			"KUBEARCHIVE_HOSTNAME":    "archive.example.svc",
			"WATSON_ENABLED":          "true",
			"WATSON_HOSTNAME":         "watson.example.com",
		})
		Expect(err).NotTo(HaveOccurred(), string(out))

		By("replacing the Tekton Results placeholder, including a trailing dot")
		tekton := readSnippet(snippets, "tekton-results.caddy")
		Expect(tekton).To(ContainSubstring("https://tekton-results.example.svc.:8080"))
		Expect(tekton).NotTo(ContainSubstring("__TEKTON_RESULTS_HOSTNAME__"))

		By("replacing the Kite placeholder, including its port")
		kite := readSnippet(snippets, "kite.caddy")
		Expect(kite).To(ContainSubstring("https://kite.example.svc:8443"))
		Expect(kite).NotTo(ContainSubstring("__KITE_HOSTNAME__"))

		By("replacing the KubeArchive placeholder")
		archive := readSnippet(snippets, "kubearchive.caddy")
		Expect(archive).To(ContainSubstring("https://archive.example.svc:8081"))
		Expect(archive).NotTo(ContainSubstring("__KUBEARCHIVE_HOSTNAME__"))

		By("replacing both Watson placeholders")
		watson := readSnippet(snippets, "watson.caddy")
		Expect(watson).To(ContainSubstring("https://watson.example.com"))
		Expect(watson).To(ContainSubstring("header_up Host watson.example.com"))
		Expect(watson).NotTo(ContainSubstring("__WATSON_HOST__"))
	})

	DescribeTable("rejects a hostname that is not a DNS name",
		func(endpointEnv map[string]string, variable, rejected string, unwritten []string) {
			By("running the script with a hostname that contains a slash")
			env := map[string]string{
				"TEMPLATES_DIR":   templates,
				"SNIPPETS_DIR":    snippets,
				"SERVICE_CA_PATH": serviceCA,
				"STATIC_DIR":      staticDir,
			}
			for key, value := range endpointEnv {
				env[key] = value
			}
			out, err := runProxyConfigScript(scriptDir, env)
			Expect(err).To(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("invalid " + variable))
			Expect(string(out)).NotTo(ContainSubstring(rejected))

			By("leaving that endpoint's snippets unwritten")
			for _, name := range unwritten {
				_, statErr := os.Stat(filepath.Join(snippets, name))
				Expect(os.IsNotExist(statErr)).To(BeTrue(), name)
			}
		},
		Entry("Tekton Results", map[string]string{
			"TEKTON_RESULTS_HOSTNAME": "results.example/extra",
		}, "TEKTON_RESULTS_HOSTNAME", "results.example/extra", []string{"tekton-results.caddy"}),
		Entry("Kite", map[string]string{
			"TEKTON_RESULTS_HOSTNAME": "tekton-results.example.svc",
			"KITE_ENABLED":            "true",
			"KITE_HOSTNAME":           "kite.example/extra",
		}, "KITE_HOSTNAME", "kite.example/extra", []string{"kite.caddy"}),
		Entry("KubeArchive", map[string]string{
			"TEKTON_RESULTS_HOSTNAME": "tekton-results.example.svc",
			"KUBEARCHIVE_ENABLED":     "true",
			"KUBEARCHIVE_HOSTNAME":    "archive.example/extra",
		}, "KUBEARCHIVE_HOSTNAME", "archive.example/extra", []string{"kubearchive.caddy"}),
		Entry("Watson", map[string]string{
			"TEKTON_RESULTS_HOSTNAME": "tekton-results.example.svc",
			"WATSON_ENABLED":          "true",
			"WATSON_HOSTNAME":         "watson.example/extra",
		}, "WATSON_HOSTNAME", "watson.example/extra", []string{"watson.caddy", "watson-tls.conf"}),
	)
})

func proxyConfigScriptDir() string {
	_, file, _, ok := runtime.Caller(0)
	Expect(ok).To(BeTrue())
	return filepath.Join(filepath.Dir(file), "../../../upstream-kustomizations/ui/core/proxy")
}

func runProxyConfigScript(scriptDir string, extra map[string]string) ([]byte, error) {
	cmd := exec.Command("bash", filepath.Join(scriptDir, "generate-proxy-config.sh")) //nolint:gosec // G204: in-repo script path
	cmd.Env = proxyConfigScriptEnv(extra)
	return cmd.CombinedOutput()
}

func proxyConfigScriptEnv(extra map[string]string) []string {
	env := make([]string, 0, 1+len(extra))
	env = append(env, "PATH="+os.Getenv("PATH"))
	for key, value := range extra {
		env = append(env, key+"="+value)
	}
	return env
}

func readSnippet(dir, name string) string {
	content, err := os.ReadFile(filepath.Join(dir, name))
	Expect(err).NotTo(HaveOccurred())
	return string(content)
}
