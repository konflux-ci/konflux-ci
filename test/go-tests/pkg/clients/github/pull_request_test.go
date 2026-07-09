package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gogithub "github.com/google/go-github/v44/github"
)

func TestIsMergeInProgress(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantResult bool
	}{
		{
			name: "405 Merge already in progress",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Merge already in progress",
			},
			wantResult: true,
		},
		{
			name: "405 merge in progress lowercase",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "merge already in progress",
			},
			wantResult: true,
		},
		{
			name: "409 head out of date is not merge in progress",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 409},
				Message:  "Head branch was modified",
			},
			wantResult: false,
		},
		{
			name: "500 server error is not merge in progress",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 500},
				Message:  "Internal Server Error",
			},
			wantResult: false,
		},
		{
			name:       "non-GitHub error",
			err:        fmt.Errorf("connection refused"),
			wantResult: false,
		},
		{
			name:       "nil error",
			err:        nil,
			wantResult: false,
		},
		{
			name: "405 with unrelated message",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Method Not Allowed",
			},
			wantResult: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				// isMergeInProgress should not panic on nil
				if got := isMergeInProgress(nil); got != tc.wantResult {
					t.Errorf("isMergeInProgress(nil) = %v, want %v", got, tc.wantResult)
				}
				return
			}
			got := isMergeInProgress(tc.err)
			if got != tc.wantResult {
				t.Errorf("isMergeInProgress(%v) = %v, want %v", tc.err, got, tc.wantResult)
			}
		})
	}
}

func TestIsPRAlreadyMerged(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantResult bool
	}{
		{
			name: "already been merged",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Pull Request has already been merged",
			},
			wantResult: true,
		},
		{
			name: "pull request is not mergeable",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Pull Request is not mergeable",
			},
			wantResult: true,
		},
		{
			name: "merge in progress is not already merged",
			err: &gogithub.ErrorResponse{
				Response: &http.Response{StatusCode: 405},
				Message:  "Merge already in progress",
			},
			wantResult: false,
		},
		{
			name:       "non-GitHub error",
			err:        fmt.Errorf("connection refused"),
			wantResult: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isPRAlreadyMerged(tc.err)
			if got != tc.wantResult {
				t.Errorf("isPRAlreadyMerged(%v) = %v, want %v", tc.err, got, tc.wantResult)
			}
		})
	}
}

// newTestClient creates a Client backed by a httptest.Server for API mocking.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	ghClient := gogithub.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(server.URL + "/")
	return &Client{client: ghClient, organization: "test-org"}, server
}

func TestMergePullRequest_405ThenMerged(t *testing.T) {
	// Simulate: first call to Merge returns 405 "Merge already in progress",
	// then GetPullRequest shows the PR as merged.
	var mergeCallCount int32
	mergeSHA := "abc123deadbeef"
	merged := true

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test-org/test-repo/pulls/42/merge", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&mergeCallCount, 1)
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Merge already in progress",
		})
	})
	mux.HandleFunc("/repos/test-org/test-repo/pulls/42", func(w http.ResponseWriter, r *http.Request) {
		pr := &gogithub.PullRequest{
			Number:         gogithub.Int(42),
			State:          gogithub.String("closed"),
			Merged:         &merged,
			MergeCommitSHA: &mergeSHA,
		}
		_ = json.NewEncoder(w).Encode(pr)
	})

	client, server := newTestClient(t, mux)
	defer server.Close()

	result, err := client.MergePullRequest("test-repo", 42)
	if err != nil {
		t.Fatalf("MergePullRequest returned error: %v", err)
	}
	if result == nil {
		t.Fatal("MergePullRequest returned nil result")
	}
	if result.GetSHA() != mergeSHA {
		t.Errorf("SHA = %q, want %q", result.GetSHA(), mergeSHA)
	}
	if !result.GetMerged() {
		t.Error("Merged = false, want true")
	}
	if atomic.LoadInt32(&mergeCallCount) != 1 {
		t.Errorf("expected Merge to be called exactly once, got %d", mergeCallCount)
	}
}

func TestMergePullRequest_AlreadyMerged(t *testing.T) {
	// Simulate: Merge returns 405 "Pull Request has already been merged",
	// then GetPullRequest confirms merged state.
	mergeSHA := "def456"
	merged := true

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test-org/test-repo/pulls/99/merge", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusMethodNotAllowed)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Pull Request has already been merged",
		})
	})
	mux.HandleFunc("/repos/test-org/test-repo/pulls/99", func(w http.ResponseWriter, r *http.Request) {
		pr := &gogithub.PullRequest{
			Number:         gogithub.Int(99),
			State:          gogithub.String("closed"),
			Merged:         &merged,
			MergeCommitSHA: &mergeSHA,
		}
		_ = json.NewEncoder(w).Encode(pr)
	})

	client, server := newTestClient(t, mux)
	defer server.Close()

	result, err := client.MergePullRequest("test-repo", 99)
	if err != nil {
		t.Fatalf("MergePullRequest returned error: %v", err)
	}
	if result.GetSHA() != mergeSHA {
		t.Errorf("SHA = %q, want %q", result.GetSHA(), mergeSHA)
	}
	if !result.GetMerged() {
		t.Error("Merged = false, want true")
	}
}

func TestMergePullRequest_Success(t *testing.T) {
	// Normal success case - no error handling needed.
	mergeSHA := "success-sha"
	merged := true

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test-org/test-repo/pulls/10/merge", func(w http.ResponseWriter, r *http.Request) {
		result := &gogithub.PullRequestMergeResult{
			SHA:    &mergeSHA,
			Merged: &merged,
		}
		_ = json.NewEncoder(w).Encode(result)
	})

	client, server := newTestClient(t, mux)
	defer server.Close()

	result, err := client.MergePullRequest("test-repo", 10)
	if err != nil {
		t.Fatalf("MergePullRequest returned error: %v", err)
	}
	if result.GetSHA() != mergeSHA {
		t.Errorf("SHA = %q, want %q", result.GetSHA(), mergeSHA)
	}
}

func TestMergePullRequest_409StillReturnsError(t *testing.T) {
	// Verify that 409 handling is preserved: error is returned (for retry by caller),
	// and branch update is attempted.
	var updateBranchCalled int32

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/test-org/test-repo/pulls/50/merge", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "409 Head branch was modified. Review and try the merge again.",
		})
	})
	mux.HandleFunc("/repos/test-org/test-repo/pulls/50/update-branch", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&updateBranchCalled, 1)
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Updating pull request branch.",
		})
	})

	client, server := newTestClient(t, mux)
	defer server.Close()

	_, err := client.MergePullRequest("test-repo", 50)
	if err == nil {
		t.Fatal("expected error for 409 response, got nil")
	}
	if atomic.LoadInt32(&updateBranchCalled) != 1 {
		t.Errorf("expected UpdatePullRequestBranch to be called once, got %d", updateBranchCalled)
	}
}
