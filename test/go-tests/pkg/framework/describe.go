package framework

import (
	ginkgo "github.com/onsi/ginkgo/v2"
)

// CommonSuiteDescribe annotates the common tests with the application label.
func CommonSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[common-suite "+text+"]", args, ginkgo.Ordered)
}

func BuildSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[build-service-suite "+text+"]", args)
}

func JVMBuildSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[jvm-build-service-suite "+text+"]", args, ginkgo.Ordered)
}

func MultiPlatformBuildSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[multi-platform-build-service-suite "+text+"]", args, ginkgo.Ordered)
}

func IntegrationServiceSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[integration-service-suite "+text+"]", args, ginkgo.Ordered)
}

func KonfluxDemoSuiteDescribe(args ...interface{}) bool {
	return ginkgo.Describe("[konflux-demo-suite]", args)
}

func EnterpriseContractSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[enterprise-contract-suite "+text+"]", args, ginkgo.Ordered)
}

func UpgradeSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[upgrade-suite "+text+"]", args, ginkgo.Ordered)
}

func ReleasePipelinesSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[release-pipelines-suite "+text+"]", args, ginkgo.Ordered)
}

func ReleaseServiceSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[release-service-suite "+text+"]", args, ginkgo.Ordered)
}

func TknBundleSuiteDescribe(text string, args ...interface{}) bool {
	return ginkgo.Describe("[task-suite "+text+"]", args, ginkgo.Ordered)
}
