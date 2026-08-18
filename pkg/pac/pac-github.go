// Package pac provides helpers for interacting with Pipelines-as-Code resources.
package pac

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v74/github"
	. "github.com/onsi/ginkgo/v2" //nolint:revive,staticcheck // dot import is idiomatic for Ginkgo
	pacv1alpha1 "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/v1alpha1"
	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/k8s"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/pipelines"
)

const (
	githubWebhookConfigName   = "github-webhook-config"
	pacEventTypeAnnotationKey = "pipelinesascode.tekton.dev/event-type"
)

// ghClient is the package-level GitHub client. Within Ordered containers this
// is safe since PAC GitHub tests run serially via SetGitHubClient.
var ghClient *github.Client

// SetGitHubClient sets the package-level GitHub client.
func SetGitHubClient(c *github.Client) {
	ghClient = c
}

// InitGitHubClient creates a GitHub client from the PAC_GITHUB_TOKEN env var.
// It skips the GitHub PAC tests if the token is not set.
func InitGitHubClient() *github.Client {
	token := os.Getenv("PAC_GITHUB_TOKEN")
	if token == "" {
		Skip("PAC_GITHUB_TOKEN not set - skipping GitHub PAC tests")
		return nil
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(context.Background(), ts)
	return github.NewClient(tc)
}

func randWebhookSecret() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func ensureWebhookSecret(c *clients.Clients, namespace, token, webhookSecret string) (func() error, error) {
	ctx := context.Background()
	secretsClient := c.KubeClient.Kube.CoreV1().Secrets(namespace)
	want := map[string]string{
		"provider.token": token,
		"webhook.secret": webhookSecret,
	}
	existing, err := secretsClient.Get(ctx, githubWebhookConfigName, metav1.GetOptions{})
	if err == nil {
		original := existing.DeepCopy()
		if existing.StringData == nil {
			existing.StringData = map[string]string{}
		}
		for k, v := range want {
			existing.StringData[k] = v
		}
		if _, err = secretsClient.Update(ctx, existing, metav1.UpdateOptions{}); err != nil {
			return nil, err
		}
		return func() error {
			current, getErr := secretsClient.Get(ctx, githubWebhookConfigName, metav1.GetOptions{})
			if apierrors.IsNotFound(getErr) {
				original.ResourceVersion = ""
				_, createErr := secretsClient.Create(ctx, original, metav1.CreateOptions{})
				return createErr
			}
			if getErr != nil {
				return getErr
			}
			original.ResourceVersion = current.ResourceVersion
			_, updateErr := secretsClient.Update(ctx, original, metav1.UpdateOptions{})
			return updateErr
		}, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	sec := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      githubWebhookConfigName,
			Namespace: namespace,
		},
		Type:       corev1.SecretTypeOpaque,
		StringData: want,
	}
	if _, err = secretsClient.Create(ctx, sec, metav1.CreateOptions{}); err != nil {
		return nil, err
	}
	return func() error {
		deleteErr := secretsClient.Delete(ctx, githubWebhookConfigName, metav1.DeleteOptions{})
		if apierrors.IsNotFound(deleteErr) {
			return nil
		}
		return deleteErr
	}, nil
}

func createGitHubRepositoryCR(c *clients.Clients, repoName, repoURL, namespace string) error {
	repo := &pacv1alpha1.Repository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "pipelinesascode.tekton.dev/v1alpha1",
			Kind:       "Repository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      repoName,
			Namespace: namespace,
		},
		Spec: pacv1alpha1.RepositorySpec{
			URL: repoURL,
			Settings: &pacv1alpha1.Settings{
				PipelineRunProvenance: "source",
			},
			GitProvider: &pacv1alpha1.GitProvider{
				Secret: &pacv1alpha1.Secret{
					Name: githubWebhookConfigName,
					Key:  "provider.token",
				},
				WebhookSecret: &pacv1alpha1.Secret{
					Name: githubWebhookConfigName,
					Key:  "webhook.secret",
				},
			},
		},
	}
	_, err := c.PacClientset.Repositories(namespace).Create(context.Background(), repo, metav1.CreateOptions{})
	return err
}

func deleteGitHubRepositoryCR(c *clients.Clients, namespace, repoName string) error {
	if repoName == "" {
		return nil
	}
	name := sanitizeGitHubK8sName(repoName)
	err := c.PacClientset.Repositories(namespace).Delete(context.Background(), name, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func deleteGitHubWebhookSecret(c *clients.Clients, namespace string) error {
	err := c.KubeClient.Kube.CoreV1().Secrets(namespace).Delete(
		context.Background(), githubWebhookConfigName, metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func deleteGitHubK8sResources(c *clients.Clients, namespace, repoName string) error {
	return errors.Join(
		deleteGitHubRepositoryCR(c, namespace, repoName),
		deleteGitHubWebhookSecret(c, namespace),
	)
}

func waitForGitHubRepoReady(ctx context.Context, owner, repo string) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		_, resp, err := ghClient.Repositories.Get(ctx, owner, repo)
		if err == nil {
			return nil
		}
		if resp != nil && resp.StatusCode == 404 {
			time.Sleep(2 * time.Second)
			continue
		}
		return err
	}
	return fmt.Errorf("timed out waiting for github repo %s/%s to be ready", owner, repo)
}

func ensureDefaultBranchMain(ctx context.Context, owner, repo, defaultBranch string) error {
	if defaultBranch == "" || defaultBranch == "main" {
		return nil
	}
	_, _, err := ghClient.Repositories.RenameBranch(ctx, owner, repo, defaultBranch, "main")
	return err
}

func addGitHubWebhook(ctx context.Context, owner, repo, smeeURL, webhookSecret string) error {
	hook := &github.Hook{
		Active: github.Ptr(true),
		Config: &github.HookConfig{
			URL:         github.Ptr(smeeURL),
			ContentType: github.Ptr("json"),
			Secret:      github.Ptr(webhookSecret),
			InsecureSSL: github.Ptr("0"),
		},
		Events: []string{"commit_comment", "issue_comment", "pull_request", "push"},
	}
	_, _, err := ghClient.Repositories.CreateHook(ctx, owner, repo, hook)
	return err
}

func sanitizeGitHubK8sName(in string) string {
	s := strings.ToLower(in)
	out := make([]byte, 0, len(s))
	for i := range s {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			out = append(out, ch)
		} else {
			out = append(out, '-')
		}
	}
	res := strings.Trim(string(out), "-")
	if res == "" {
		return "pac-repo"
	}
	if len(res) > 63 {
		return res[:63]
	}
	return res
}

func createBranchWithGitHub(ctx context.Context, owner, repo, baseBranch, newBranch, message string, files map[string]string) error {
	baseRef, _, err := ghClient.Git.GetRef(ctx, owner, repo, "refs/heads/"+baseBranch)
	if err != nil {
		return err
	}
	baseCommitSHA := baseRef.GetObject().GetSHA()
	if baseCommitSHA == "" {
		return fmt.Errorf("base branch %q has empty SHA", baseBranch)
	}

	baseCommit, _, err := ghClient.Git.GetCommit(ctx, owner, repo, baseCommitSHA)
	if err != nil {
		return err
	}
	baseTreeSHA := ""
	if baseCommit.Tree != nil {
		baseTreeSHA = baseCommit.Tree.GetSHA()
	}
	if baseTreeSHA == "" {
		return fmt.Errorf("base commit %s has empty tree SHA", baseCommitSHA)
	}

	entries := make([]*github.TreeEntry, 0, len(files))
	for p, c := range files {
		path := p
		content := c
		entries = append(entries, &github.TreeEntry{
			Path:    &path,
			Mode:    github.Ptr("100644"),
			Type:    github.Ptr("blob"),
			Content: &content,
		})
	}

	newTree, _, err := ghClient.Git.CreateTree(ctx, owner, repo, baseTreeSHA, entries)
	if err != nil {
		return err
	}

	commit := &github.Commit{
		Message: github.Ptr(message),
		Tree:    newTree,
		Parents: []*github.Commit{{SHA: github.Ptr(baseCommitSHA)}},
	}
	newCommit, _, err := ghClient.Git.CreateCommit(ctx, owner, repo, commit, nil)
	if err != nil {
		return err
	}
	newCommitSHA := newCommit.GetSHA()
	if newCommitSHA == "" {
		return fmt.Errorf("created commit has empty SHA")
	}

	ref := &github.Reference{
		Ref: github.Ptr("refs/heads/" + newBranch),
		Object: &github.GitObject{
			SHA: github.Ptr(newCommitSHA),
		},
	}
	_, _, err = ghClient.Git.CreateRef(ctx, owner, repo, ref)
	return err
}

// SetupGitHubProject creates a new GitHub repository, configures the webhook secret,
// creates the PAC Repository CR, and adds the GitHub webhook pointing to smeeURL.
// Returns the resolved owner login, repository name, and any error.
func SetupGitHubProject(c *clients.Clients, namespace, smeeURL string) (owner, repoName string, err error) {
	if ghClient == nil {
		return "", "", fmt.Errorf("github client not initialized; call InitGitHubClient/SetGitHubClient first")
	}

	ctx := context.Background()
	org := os.Getenv("PAC_GITHUB_ORG")
	token := os.Getenv("PAC_GITHUB_TOKEN")

	webhookSecret := os.Getenv("PAC_GITHUB_WEBHOOK_TOKEN")
	if webhookSecret == "" {
		sec, secErr := randWebhookSecret()
		if secErr != nil {
			return "", "", fmt.Errorf("failed generating github webhook secret: %w", secErr)
		}
		webhookSecret = sec
	}

	repoName = fmt.Sprintf("release-tests-pac-%08d", time.Now().UnixNano()%1e8)
	createReq := &github.Repository{
		Name:                github.Ptr(repoName),
		Visibility:          github.Ptr("public"),
		AutoInit:            github.Ptr(true),
		AllowSquashMerge:    github.Ptr(true),
		DeleteBranchOnMerge: github.Ptr(true),
	}
	created, _, createErr := ghClient.Repositories.Create(ctx, org, createReq)
	if createErr != nil {
		return "", "", fmt.Errorf("failed to create github repository: %w", createErr)
	}

	rollbackOwner := org
	if rollbackOwner == "" {
		rollbackOwner = created.GetOwner().GetLogin()
	}
	rollbackRepo := repoName
	var secretRollback func() error
	repositoryCreated := false
	setupComplete := false
	defer func() {
		if setupComplete {
			return
		}
		if repositoryCreated {
			if cleanupErr := deleteGitHubRepositoryCR(c, namespace, rollbackRepo); cleanupErr != nil {
				log.Printf("WARNING: failed to clean up PAC Repository: %v", cleanupErr)
			}
		}
		if secretRollback != nil {
			if cleanupErr := secretRollback(); cleanupErr != nil {
				log.Printf("WARNING: failed to restore GitHub PAC credentials: %v", cleanupErr)
			}
		}
		if rollbackOwner != "" {
			if _, deleteErr := ghClient.Repositories.Delete(ctx, rollbackOwner, rollbackRepo); deleteErr != nil {
				log.Printf("WARNING: failed to delete GitHub repo during rollback: %v", deleteErr)
			}
		}
	}()

	switch {
	case org != "":
		owner = org
	case created.Owner != nil && created.Owner.Login != nil:
		owner = created.GetOwner().GetLogin()
	default:
		u, _, uerr := ghClient.Users.Get(ctx, "")
		if uerr != nil {
			return "", "", fmt.Errorf("failed to determine github username: %w", uerr)
		}
		owner = u.GetLogin()
		rollbackOwner = owner
	}

	if waitErr := waitForGitHubRepoReady(ctx, owner, repoName); waitErr != nil {
		return "", "", waitErr
	}
	if branchErr := ensureDefaultBranchMain(ctx, owner, repoName, created.GetDefaultBranch()); branchErr != nil {
		return "", "", fmt.Errorf("failed to rename default branch to main: %w", branchErr)
	}

	repoURL := created.GetHTMLURL()
	if repoURL == "" {
		repoURL = fmt.Sprintf("https://github.com/%s/%s", owner, repoName)
	}

	var secretErr error
	secretRollback, secretErr = ensureWebhookSecret(c, namespace, token, webhookSecret)
	if secretErr != nil {
		return "", "", fmt.Errorf("failed to ensure github webhook secret: %w", secretErr)
	}
	if crErr := createGitHubRepositoryCR(c, sanitizeGitHubK8sName(repoName), repoURL, namespace); crErr != nil {
		return "", "", fmt.Errorf("failed to create PAC Repository CR: %w", crErr)
	}
	repositoryCreated = true
	if hookErr := addGitHubWebhook(ctx, owner, repoName, smeeURL, webhookSecret); hookErr != nil {
		return "", "", fmt.Errorf("failed to add github webhook: %w", hookErr)
	}

	projectURL = repoURL
	log.Printf("GitHub repo created: %s", strings.ReplaceAll(strings.ReplaceAll(repoURL, "\n", ""), "\r", "")) //nolint:gosec // repoURL comes from GitHub API or is built from sanitized owner+name
	setupComplete = true
	return owner, repoName, nil
}

func waitForPRMergeable(ctx context.Context, owner, repo string, number int) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		pr, _, err := ghClient.PullRequests.Get(ctx, owner, repo, number)
		if err != nil {
			return err
		}
		if pr.Mergeable != nil && *pr.Mergeable {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for PR #%d to become mergeable", number)
}

// ConfigurePreviewChangesGitHub creates a branch containing the generated .tekton pipeline
// files and opens a pull request targeting main.
// Returns the PR web URL, PR number, and any error.
func ConfigurePreviewChangesGitHub(owner, repo string) (prURL string, prNumber int, err error) {
	ctx := context.Background()

	prPath := pullRequestFile()
	pushPath := pushFile()
	prData, readErr := os.ReadFile(filepath.Clean(prPath))
	if readErr != nil {
		return "", 0, fmt.Errorf("read %s: %w", prPath, readErr)
	}
	pushData, pushErr := os.ReadFile(filepath.Clean(pushPath))
	hasPush := pushErr == nil

	branchName := fmt.Sprintf("preview-%08d", time.Now().UnixNano()%1e8)
	files := map[string]string{
		".tekton/pull-request.yaml": string(prData),
	}
	if hasPush {
		files[".tekton/push.yaml"] = string(pushData)
	}

	if branchErr := createBranchWithGitHub(ctx, owner, repo, "main", branchName, "ci(pac): add pipelines-as-code definitions", files); branchErr != nil {
		return "", 0, fmt.Errorf("failed to create branch %q: %w", branchName, branchErr)
	}

	newPR := &github.NewPullRequest{
		Title: github.Ptr("Add preview changes for feature"),
		Head:  github.Ptr(owner + ":" + branchName),
		Base:  github.Ptr("main"),
	}
	pr, _, prCreateErr := ghClient.PullRequests.Create(ctx, owner, repo, newPR)
	if prCreateErr != nil {
		return "", 0, fmt.Errorf("failed to create PR: %w", prCreateErr)
	}

	log.Printf("Pull Request Created: %s", pr.GetHTMLURL())
	return pr.GetHTMLURL(), pr.GetNumber(), nil
}

// TriggerPushOnGitHubMain waits for the PR to become mergeable and squash-merges it,
// which triggers a push PipelineRun on the main branch.
func TriggerPushOnGitHubMain(owner, repo string, prNum int) error {
	ctx := context.Background()
	if err := waitForPRMergeable(ctx, owner, repo, prNum); err != nil {
		return err
	}
	_, _, err := ghClient.PullRequests.Merge(ctx, owner, repo, prNum, "", &github.PullRequestOptions{
		MergeMethod: "squash",
	})
	if err != nil {
		return fmt.Errorf("failed to merge PR #%d: %w", prNum, err)
	}
	return nil
}

// WaitForNewPipelineRunName polls until a PipelineRun name different from previousName
// appears in the namespace. Set previousName to "" to wait for the first PipelineRun.
func WaitForNewPipelineRunName(c *clients.Clients, namespace, previousName string) (string, error) {
	deadline := time.Now().Add(config.APITimeout)
	for time.Now().Before(deadline) {
		name, err := pipelines.GetLatestPipelinerun(c, namespace)
		if err == nil {
			if previousName == "" || name != previousName {
				return name, nil
			}
		}
		time.Sleep(config.APIRetry)
	}
	if previousName == "" {
		return "", fmt.Errorf("timed out waiting for a PipelineRun to be created in namespace %q", namespace)
	}
	return "", fmt.Errorf("timed out waiting for a new PipelineRun in namespace %q (previous=%q)", namespace, previousName)
}

// WaitForNewPipelineRunNameByEventType polls for a PipelineRun annotated with the given
// PAC event-type that is different from previousName. Set previousName to "" to match any.
func WaitForNewPipelineRunNameByEventType(c *clients.Clients, namespace, previousName, eventType string) (string, error) {
	deadline := time.Now().Add(config.APITimeout)
	for time.Now().Before(deadline) {
		prs, err := c.PipelineRunClient.List(c.Ctx, metav1.ListOptions{})
		if err == nil {
			var bestName string
			var bestStart time.Time
			var bestFound bool
			for _, pr := range prs.Items {
				if pr.Annotations == nil {
					continue
				}
				if pr.Annotations[pacEventTypeAnnotationKey] != eventType {
					continue
				}
				if previousName != "" && pr.Name == previousName {
					continue
				}
				start := pr.CreationTimestamp.Time
				if pr.Status.StartTime != nil {
					start = pr.Status.StartTime.Time
				}
				if !bestFound || start.After(bestStart) {
					bestFound = true
					bestStart = start
					bestName = pr.Name
				}
			}
			if bestFound {
				return bestName, nil
			}
		}
		time.Sleep(config.APIRetry)
	}
	return "", fmt.Errorf("timed out waiting for a new PipelineRun with event-type=%q in namespace %q (previous=%q)", eventType, namespace, previousName)
}

// CleanupPACGitHub removes generated files, credentials, cluster resources,
// the GitHub repository, and the Smee deployment.
func CleanupPACGitHub(c *clients.Clients, namespace, smeeDeploymentName, owner, repo string) error {
	_ = os.Remove(pullRequestFile())
	_ = os.Remove(pushFile())

	var errs []error
	if cleanupErr := deleteGitHubK8sResources(c, namespace, repo); cleanupErr != nil {
		errs = append(errs, cleanupErr)
	}
	if owner != "" && repo != "" && ghClient != nil {
		if _, deleteErr := ghClient.Repositories.Delete(context.Background(), owner, repo); deleteErr != nil {
			errs = append(errs, fmt.Errorf("delete github repository %s/%s: %w", owner, repo, deleteErr))
		}
	}
	if err := k8s.DeleteDeployment(c, namespace, smeeDeploymentName); err != nil {
		errs = append(errs, fmt.Errorf("delete smee deployment: %w", err))
	}
	return errors.Join(errs...)
}
