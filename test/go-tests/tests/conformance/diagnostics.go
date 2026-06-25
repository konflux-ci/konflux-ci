package conformance

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/constants"
	"github.com/konflux-ci/konflux-ci/test/go-tests/pkg/framework"
	releaseApi "github.com/konflux-ci/release-service/api/v1alpha1"
	tektonutils "github.com/konflux-ci/release-service/tekton/utils"
	pipeline "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

const (
	githubAPIBodyLogLimit        = 200
	githubAPIProbeTimeout        = 5 * time.Second
	collectDataGitHubFlakeMarker = "diagnostic: known-flake collect-data-github-api"
	// collect-data logs the curl line and jq parse error back-to-back; ignore distant pairings.
	collectDataGitHubAPIFlakeParseErrorWindow = 512
)

// probedGitHubAPI tracks PipelineRuns for which the outbound GitHub probe already ran.
// The release failure path is polled by Eventually; probing once avoids repeated
// outbound calls and extra load on shared CI egress rate limits.
var probedGitHubAPI sync.Map

// collectDataGitHubAPIFlakeRe matches the unauthenticated GitHub commit lookup in
// release-service-catalog collect-data when jq fails on a non-JSON response.
var collectDataGitHubAPIFlakeRe = regexp.MustCompile(`api\.github\.com/repos/[^\s"']+/commits/[0-9a-f]+`)

// logReleaseFailureDiagnostics records cluster state to help triage release PipelineRun
// failures. Uses klog because GinkgoWriter is suppressed during Eventually retries.
func logReleaseFailureDiagnostics(
	hub *framework.ControllerHub,
	pr *pipeline.PipelineRun,
	release *releaseApi.Release,
	managedNamespace string,
	failedLogs string,
) {
	if pr == nil {
		return
	}

	klog.Errorf("diagnostic: release PipelineRun %s/%s failure details", pr.Namespace, pr.Name)
	for _, c := range pr.Status.Conditions {
		klog.Errorf("diagnostic:   PipelineRun condition type=%s status=%s reason=%s message=%s",
			c.Type, c.Status, c.Reason, c.Message)
	}

	logReleasePipelineRunTaskRuns(hub, pr)

	if release != nil {
		logReleaseCRDiagnostics(hub, release)
		logReleasePlanAdmissionDiagnostics(hub, release, failedLogs, pipelineRunKey(pr))
	} else {
		klog.Errorf("diagnostic: Release CR unavailable (nil)")
	}

	dumpManagedPipelineRuns(hub, managedNamespace)
}

func logReleasePipelineRunTaskRuns(hub *framework.ControllerHub, pr *pipeline.PipelineRun) {
	client := hub.ReleaseController.KubeRest()
	for _, chr := range pr.Status.ChildReferences {
		taskRun := &pipeline.TaskRun{}
		key := types.NamespacedName{Namespace: pr.Namespace, Name: chr.Name}
		if err := client.Get(context.Background(), key, taskRun); err != nil {
			klog.Errorf("diagnostic:   TaskRun %s/%s: get error: %v", key.Namespace, key.Name, err)
			continue
		}

		klog.Infof("diagnostic:   TaskRun %s/%s pipelineTask=%q pod=%s",
			taskRun.Namespace, taskRun.Name, chr.PipelineTaskName, taskRun.Status.PodName)
		for _, tc := range taskRun.Status.Conditions {
			klog.Infof("diagnostic:     condition type=%s status=%s reason=%s message=%s",
				tc.Type, tc.Status, tc.Reason, tc.Message)
		}
		for _, step := range taskRun.Status.Steps {
			if step.Terminated != nil {
				klog.Infof("diagnostic:     step %q container=%s reason=%s exitCode=%d message=%q",
					step.Name, step.Container, step.Terminated.Reason, step.Terminated.ExitCode, step.Terminated.Message)
			} else if step.Running != nil {
				klog.Infof("diagnostic:     step %q container=%s running", step.Name, step.Container)
			} else if step.Waiting != nil {
				klog.Infof("diagnostic:     step %q container=%s waiting reason=%s message=%q",
					step.Name, step.Container, step.Waiting.Reason, step.Waiting.Message)
			}
		}
	}
}

func logReleaseCRDiagnostics(hub *framework.ControllerHub, release *releaseApi.Release) {
	fresh, err := hub.ReleaseController.GetRelease(release.Name, "", release.Namespace)
	if err != nil {
		klog.Errorf("diagnostic: Release %s/%s: re-fetch error: %v", release.Namespace, release.Name, err)
		return
	}

	klog.Infof("diagnostic: Release %s/%s spec.releasePlan=%q spec.snapshot=%q",
		fresh.Namespace, fresh.Name, fresh.Spec.ReleasePlan, fresh.Spec.Snapshot)
	for _, c := range fresh.Status.Conditions {
		klog.Infof("diagnostic:   Release condition type=%s status=%s reason=%s message=%s",
			c.Type, c.Status, c.Reason, c.Message)
	}
	klog.Infof("diagnostic:   Release collectorsProcessing managed=%+v tenant=%+v",
		fresh.Status.CollectorsProcessing.ManagedCollectorsProcessing,
		fresh.Status.CollectorsProcessing.TenantCollectorsProcessing)
}

func logReleasePlanAdmissionDiagnostics(
	hub *framework.ControllerHub,
	release *releaseApi.Release,
	failedLogs string,
	pipelineRunKey string,
) {
	releasePlanName := release.Spec.ReleasePlan
	if releasePlanName == "" {
		klog.Errorf("diagnostic: ReleasePlan name unknown (empty release.spec.releasePlan)")
		return
	}

	releasePlan, err := hub.ReleaseController.GetReleasePlan(releasePlanName, release.Namespace)
	if err != nil {
		klog.Errorf("diagnostic: ReleasePlan %s/%s: get error: %v", release.Namespace, releasePlanName, err)
		return
	}

	matched := strings.TrimSpace(releasePlan.Status.ReleasePlanAdmission.Name)
	if matched == "" {
		klog.Errorf("diagnostic: ReleasePlan %s/%s has no matched ReleasePlanAdmission in status",
			release.Namespace, releasePlanName)
		return
	}

	rpaNamespace, rpaName, ok := parseNamespacedName(matched)
	if !ok {
		klog.Errorf("diagnostic: ReleasePlan %s/%s status.releasePlanAdmission.name %q is not a valid namespaced name",
			release.Namespace, releasePlanName, matched)
		return
	}

	rpa, err := hub.ReleaseController.GetReleasePlanAdmission(rpaName, rpaNamespace)
	if err != nil {
		klog.Errorf("diagnostic: ReleasePlanAdmission %s/%s: get error: %v", rpaNamespace, rpaName, err)
		return
	}

	if rpa.Spec.Pipeline == nil {
		klog.Infof("diagnostic: ReleasePlanAdmission %s/%s has no pipeline spec", rpaNamespace, rpaName)
		return
	}

	ref := rpa.Spec.Pipeline.PipelineRef
	klog.Infof("diagnostic: ReleasePlanAdmission %s/%s pipelineRef resolver=%q ociStorage=%q useEmptyDir=%v",
		rpaNamespace, rpaName, ref.Resolver, ref.OciStorage, ref.UseEmptyDir)
	for _, p := range ref.Params {
		klog.Infof("diagnostic:   pipelineRef param %s=%q", p.Name, p.Value)
	}

	probeGitHubCommitAPIIfNeeded(failedLogs, ref, pipelineRunKey)
}

func pipelineRunKey(pr *pipeline.PipelineRun) string {
	return pr.Namespace + "/" + pr.Name
}

func dumpManagedPipelineRuns(hub *framework.ControllerHub, managedNamespace string) {
	prs, err := hub.TektonController.ListAllPipelineRuns(managedNamespace)
	if err != nil {
		klog.Errorf("diagnostic: could not list PipelineRuns in %s: %v", managedNamespace, err)
		return
	}

	klog.Infof("diagnostic: PipelineRuns in %s: %d", managedNamespace, len(prs.Items))
	for _, pr := range prs.Items {
		status := "Pending"
		for _, c := range pr.Status.Conditions {
			status = fmt.Sprintf("%s (reason: %s, message: %s)", c.Status, c.Reason, c.Message)
		}
		klog.Infof("diagnostic:   - %s release=%s status=%s",
			pr.Name,
			pr.Labels["release.appstudio.openshift.io/release"],
			status)
	}
}

func probeGitHubCommitAPIIfNeeded(failedLogs string, ref tektonutils.PipelineRef, pipelineRunKey string) {
	if !isCollectDataGitHubAPIFlake(failedLogs) {
		return
	}
	if githubAPIAlreadyProbed(pipelineRunKey) {
		return
	}

	gitURL := pipelineRefParam(ref, "url")
	revision := pipelineRefParam(ref, "revision")
	pathInRepo := pipelineRefParam(ref, "pathInRepo")
	org, repo, ok := parseGitHubOrgRepo(gitURL)
	if !ok {
		klog.Errorf("%s: could not parse org/repo from pipelineRef url %q", collectDataGitHubFlakeMarker, gitURL)
		return
	}

	klog.Errorf("%s org=%s repo=%s revision=%s pathInRepo=%q",
		collectDataGitHubFlakeMarker, org, repo, revision, pathInRepo)

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s", org, repo, revision)
	probeGitHubCommitAPI(apiURL, "unauthenticated", "")
	if token := strings.TrimSpace(os.Getenv(constants.GITHUB_TOKEN_ENV)); token != "" {
		probeGitHubCommitAPI(apiURL, "authenticated", token)
	} else {
		klog.Infof("diagnostic: GitHub API probe skipped authenticated request (%s not set)", constants.GITHUB_TOKEN_ENV)
	}
}

func isCollectDataGitHubAPIFlake(logs string) bool {
	match := collectDataGitHubAPIFlakeRe.FindStringIndex(logs)
	if match == nil {
		return false
	}
	after := logs[match[1]:]
	if len(after) > collectDataGitHubAPIFlakeParseErrorWindow {
		after = after[:collectDataGitHubAPIFlakeParseErrorWindow]
	}
	return strings.Contains(after, "parse error")
}

func githubAPIAlreadyProbed(pipelineRunKey string) bool {
	_, loaded := probedGitHubAPI.LoadOrStore(pipelineRunKey, true)
	return loaded
}

func parseNamespacedName(name string) (namespace, objName string, ok bool) {
	name = strings.TrimSpace(name)
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func pipelineRefParam(ref tektonutils.PipelineRef, name string) string {
	for _, p := range ref.Params {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

func parseGitHubOrgRepo(gitURL string) (org, repo string, ok bool) {
	gitURL = strings.TrimSpace(gitURL)
	gitURL = strings.TrimSuffix(gitURL, ".git")
	const marker = "github.com/"
	idx := strings.Index(gitURL, marker)
	if idx < 0 {
		return "", "", false
	}
	path := strings.Trim(gitURL[idx+len(marker):], "/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func probeGitHubCommitAPI(apiURL, label, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), githubAPIProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		klog.Errorf("diagnostic: GitHub API probe (%s): build request: %v", label, err)
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "konflux-conformance-e2e")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		klog.Errorf("diagnostic: GitHub API probe (%s) GET %s: %v", label, apiURL, err)
		return
	}
	defer resp.Body.Close()

	// Unauthenticated probe: log a short body prefix to spot non-JSON/HTML responses
	// (the collect-data jq failure mode). Authenticated responses can include full
	// commit metadata and are omitted from CI logs; status and rate-limit headers suffice.
	if token != "" {
		klog.Errorf("diagnostic: GitHub API probe (%s) GET %s: status=%d retry-after=%q x-ratelimit-limit=%q x-ratelimit-remaining=%q x-ratelimit-reset=%q x-ratelimit-resource=%q",
			label,
			apiURL,
			resp.StatusCode,
			resp.Header.Get("Retry-After"),
			resp.Header.Get("X-Ratelimit-Limit"),
			resp.Header.Get("X-Ratelimit-Remaining"),
			resp.Header.Get("X-Ratelimit-Reset"),
			resp.Header.Get("X-Ratelimit-Resource"),
		)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, githubAPIBodyLogLimit))
	if err != nil {
		klog.Errorf("diagnostic: GitHub API probe (%s) GET %s: read body: %v", label, apiURL, err)
		return
	}

	klog.Errorf("diagnostic: GitHub API probe (%s) GET %s: status=%d retry-after=%q x-ratelimit-limit=%q x-ratelimit-remaining=%q x-ratelimit-reset=%q x-ratelimit-resource=%q body-prefix=%q",
		label,
		apiURL,
		resp.StatusCode,
		resp.Header.Get("Retry-After"),
		resp.Header.Get("X-Ratelimit-Limit"),
		resp.Header.Get("X-Ratelimit-Remaining"),
		resp.Header.Get("X-Ratelimit-Reset"),
		resp.Header.Get("X-Ratelimit-Resource"),
		strings.TrimSpace(string(body)),
	)
}
