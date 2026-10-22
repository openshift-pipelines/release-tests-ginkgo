package pac

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-github/v74/github"
)

func TestCallerPipelineIsCommittedExactlyAndPRIdentifiesThatCommit(t *testing.T) {
	content := "apiVersion: tekton.dev/v1\nkind: PipelineRun\nmetadata:\n  generateName: caller-supplied-\n"
	var committed string
	var branch string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo/git/ref/heads/main":
			_, _ = w.Write([]byte(`{"object":{"sha":"base"}}`))
		case "GET /repos/owner/repo/git/commits/base":
			_, _ = w.Write([]byte(`{"tree":{"sha":"base-tree"}}`))
		case "POST /repos/owner/repo/git/trees":
			var tree github.Tree
			if err := json.NewDecoder(r.Body).Decode(&tree); err != nil {
				t.Error(err)
				return
			}
			if len(tree.Entries) != 1 || tree.Entries[0].GetPath() != ".tekton/pull-request.yaml" || tree.Entries[0].GetMode() != "100644" {
				t.Error("committed unrelated files or wrong fixture path")
			}
			committed = tree.Entries[0].GetContent()
			_, _ = w.Write([]byte(`{"sha":"tree"}`))
		case "POST /repos/owner/repo/git/commits":
			var commit struct {
				Tree    string   `json:"tree"`
				Parents []string `json:"parents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&commit); err != nil {
				t.Error(err)
				return
			}
			if commit.Tree != "tree" || len(commit.Parents) != 1 || commit.Parents[0] != "base" {
				t.Error("did not reuse branch/commit plumbing")
			}
			_, _ = w.Write([]byte(`{"sha":"head-sha","tree":{"sha":"tree"}}`))
		case "POST /repos/owner/repo/git/refs":
			var ref struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			}
			if err := json.NewDecoder(r.Body).Decode(&ref); err != nil {
				t.Error(err)
				return
			}
			branch = strings.TrimPrefix(ref.Ref, "refs/heads/")
			if !strings.HasPrefix(branch, "preview-") || ref.SHA != "head-sha" {
				t.Error("wrong preview ref")
			}
			_, _ = w.Write([]byte(`{"ref":"` + ref.Ref + `","object":{"sha":"head-sha"}}`))
		case "POST /repos/owner/repo/pulls":
			var request github.NewPullRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				return
			}
			if request.GetHead() != "owner:"+branch || request.GetBase() != "main" {
				t.Error("wrong pull request source/base")
			}
			_, _ = w.Write([]byte(`{"number":7,"html_url":"https://github.com/owner/repo/pull/7","head":{"sha":"head-sha"}}`))
		default:
			t.Errorf("unexpected GitHub operation %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	previous := ghClient
	t.Cleanup(func() { ghClient = previous })
	ghClient = github.NewClient(server.Client())
	base, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	ghClient.BaseURL = base
	pr, err := ConfigurePreviewPipelineGitHub(t.Context(), "owner", "repo", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if committed != content || pr.GetNumber() != 7 || pr.GetHead().GetSHA() != "head-sha" {
		t.Fatal("caller content or event commit identity changed")
	}
}

func TestCallerPipelineRejectsEmptyInputAndHonorsCancellation(t *testing.T) {
	if _, err := ConfigurePreviewPipelineGitHub(t.Context(), "owner", "repo", nil); err == nil {
		t.Fatal("accepted empty definition")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("canceled operation reached GitHub") }))
	defer server.Close()
	previous := ghClient
	t.Cleanup(func() { ghClient = previous })
	ghClient = github.NewClient(server.Client())
	base, err := url.Parse(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	ghClient.BaseURL = base
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ConfigurePreviewPipelineGitHub(ctx, "owner", "repo", []byte("definition")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity = %v", err)
	}
}
