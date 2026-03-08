package framework

import (
	"regexp"
	"strings"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/logs"

	ginkgo "github.com/onsi/ginkgo/v2"
)

func ReportFailure(f **Framework) func() {
	namespaces := map[string]string{
		"Build Service":       "build-service",
		"JVM Build Service":   "jvm-build-service",
		"Application Service": "application-service",
		"Image Controller":    "image-controller"}

	return func() {
		if !ginkgo.CurrentSpecReport().Failed() {
			return
		}

		fwk := *f
		if fwk == nil {
			return
		}

		if err := logs.StoreTestTiming(); err != nil {
			ginkgo.GinkgoWriter.Printf("failed to store test timing: %v\n", err)
		}

		allPodLogs := make(map[string][]byte)
		for _, namespace := range namespaces {
			podList, err := fwk.AsKubeAdmin.CommonController.ListAllPods(namespace)
			if err != nil {
				ginkgo.GinkgoWriter.Printf("failed to list pods in namespace %s: %v\n", namespace, err)
				return
			}

			for _, pod := range podList.Items {
				podLogs := fwk.AsKubeAdmin.CommonController.GetPodLogs(&pod)

				for podName, log := range podLogs {
					if filteredLogs := FilterLogs(string(log), ginkgo.CurrentSpecReport().StartTime); filteredLogs != "" {
						allPodLogs[podName] = []byte(filteredLogs)
					}
				}
			}
		}

		if err := logs.StoreArtifacts(allPodLogs); err != nil {
			ginkgo.GinkgoWriter.Printf("failed to store pod logs: %v\n", err)
		}
	}
}

func FilterLogs(logs string, start time.Time) string {

	//bit of a hack, the logs are in different formats and are not always valid JSON
	//just look for RFC 3339 dates line by line, once we find one after the start time dump the
	//rest of the lines
	lines := strings.Split(logs, "\n")
	ret := []string{}
	rfc3339Pattern := `(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2}))`

	re := regexp.MustCompile(rfc3339Pattern)
	for pos, i := range lines {
		match := re.FindStringSubmatch(i)

		if match != nil {
			dateString := match[1]
			ts, err := time.Parse(time.RFC3339, dateString)
			if err != nil {
				ret = append(ret, "Invalid Time, unable to parse date: "+i)
			} else if ts.Equal(start) || ts.After(start) {
				ret = append(ret, lines[pos:]...)
				break
			}
		}
	}

	return strings.Join(ret, "\n")
}
