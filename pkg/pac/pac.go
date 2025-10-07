package pac

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	cli "github.com/srivickynesh/release-tests-ginkgo/pkg/clients"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/cmd"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/config"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/k8s"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/oc"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/opc"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/store"
	"github.com/xanzy/go-gitlab"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	pacv1alpha1 "github.com/openshift-pipelines/pipelines-as-code/pkg/apis/pipelinesascode/v1alpha1"
)

const (
	webhookConfigName = "gitlab-webhook-config"
	targetURL         = "http://pipelines-as-code-controller.openshift-pipelines:8080"
)

const (
	maxRetriesForkProject = 5
)

var client *gitlab.Client

func SetGitLabClient(c *gitlab.Client) {
	client = c
}

// Initialize Gitlab Client
func InitGitLabClient() *gitlab.Client {
	tokenSecretData := os.Getenv("GITLAB_TOKEN")
	webhookSecretData := os.Getenv("GITLAB_WEBHOOK_TOKEN")
	if tokenSecretData == "" && webhookSecretData == "" {
		fmt.Errorf("token for authorization to the GitLab repository was not exported as a system variable")
	} else {
		if !oc.SecretExists(webhookConfigName, store.Namespace()) {
			oc.CreateSecretForWebhook(tokenSecretData, webhookSecretData, store.Namespace())
		} else {
			log.Printf("Secret %q already exists", webhookConfigName)
		}
	}
	client, err := gitlab.NewClient(tokenSecretData)
	if err != nil {
		Fail(fmt.Sprintf("failed to initialize GitLab client: %v", err))
	}

	return client
}

func AssertPACInfoInstall() {
	pacInfo, err := opc.GetOpcPacInfoInstall()
	if err != nil {
		Fail(fmt.Sprintf("failed to get pac info: %v", err))
		return
	}

	clusterVersion := pacInfo.PipelinesAsCode.InstallVersion
	expectedVersion := os.Getenv("PAC_VERSION")

	if !strings.Contains(clusterVersion, expectedVersion) ||
		pacInfo.PipelinesAsCode.InstallNamespace != config.TargetNamespace {
		Fail(fmt.Sprintf("PAC version %s doesn't match the expected version %s or namespace %s is wrong",
			clusterVersion, expectedVersion, pacInfo.PipelinesAsCode.InstallNamespace))
	}
}

func SetupSmeeDeployment() {
	var err error
	smeeDeploymentName := "gosmee-client"
	store.PutScenarioData("smeeDeploymentName", smeeDeploymentName)

	smeeURL, err := getNewSmeeURL()
	if err != nil {
		Fail(fmt.Sprintf("failed to get a new Smee URL: %v", err))
		return
	}
	store.PutScenarioData("SMEE_URL", smeeURL)

	if err = createSmeeDeployment(store.Clients(), store.Namespace(), smeeURL); err != nil {
		Fail(fmt.Sprintf("failed to create deployment: %v", err))
	}
}

func getNewSmeeURL() (string, error) {
	res := cmd.Run("curl", "-s", "https://smee.io/new")
	if res.ExitCode != 0 {
		return "", fmt.Errorf("failed to get a new Smee URL: %s", res.Stderr())
	}
	smeeURL := strings.TrimSpace(res.Stdout())
	if smeeURL == "" {
		return "", fmt.Errorf("failed to retrieve Smee URL: no URL found in output")
	}
	return smeeURL, nil
}

func createSmeeDeployment(c *cli.Clients, namespace, smeeURL string) error {
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gosmee-client",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "gosmee-client",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "gosmee-client",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "gosmee-client",
							Image: "ghcr.io/chmouel/gosmee:latest",
							Command: []string{
								"gosmee",
								"client",
								smeeURL,
								targetURL,
							},
							Env: []corev1.EnvVar{
								{
									Name:  "SMEE_URL",
									Value: smeeURL,
								},
								{
									Name:  "TARGET_URL",
									Value: targetURL,
								},
							},
						},
					},
				},
			},
		},
	}

	kc := c.KubeClient.Kube
	deploymentsClient := kc.AppsV1().Deployments(namespace)
	result, err := deploymentsClient.Create(context.TODO(), deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create deployment: %v", err)
	}

	log.Printf("Created deployment %q in namespace %q.", result.GetObjectMeta().GetName(), namespace)
	return nil
}

func SetupGitLabProject() *gitlab.Project {
	gitlabGroupNamespace := os.Getenv("GITLAB_GROUP_NAMESPACE")
	projectIDOrPath := os.Getenv("GITLAB_PROJECT_ID")

	if gitlabGroupNamespace == "" || projectIDOrPath == "" {
		Fail(fmt.Errorf("failed to get system variables").Error())
	}

	smeeURL := store.GetScenarioData("SMEE_URL").(string)
	webhookToken := os.Getenv("GITLAB_WEBHOOK_TOKEN")

	project, err := forkProject(projectIDOrPath, gitlabGroupNamespace)
	if err != nil {
		Fail(fmt.Errorf("error during project forking: %w", err).Error())
	}

	err = addWebhook(project.ID, smeeURL, webhookToken)
	if err != nil {
		Fail(fmt.Errorf("failed to add webhook: %w", err).Error())
	}

	err = createNewRepository(store.Clients(), project.Name, gitlabGroupNamespace, store.Namespace())
	if err != nil {
		Fail(fmt.Errorf("failed to create repository").Error())
	}
	store.PutScenarioData("projectID", strconv.Itoa(project.ID))

	return project
}

// Specified Gitlab Project ID is forked into Group Namespace
func forkProject(projectID, targetNamespace string) (*gitlab.Project, error) {
	for i := 0; i < maxRetriesForkProject; i++ {
		projectName := fmt.Sprintf("release-tests-fork-%08d", time.Now().UnixNano()%1e8)
		project, _, err := client.Projects.ForkProject(projectID, &gitlab.ForkProjectOptions{
			Namespace: &targetNamespace,
			Name:      &projectName,
			Path:      &projectName,
		})
		if err == nil {
			store.PutScenarioData("PROJECT_URL", project.WebURL)
			store.PutScenarioData("projectName", project.Name)
			return project, nil
		}
		log.Printf("Retry %d: failed to fork project: %v", i+1, err)
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("failed to fork project after %d attempts", maxRetriesForkProject)
}

// Add WebhookURL to forked Project
func addWebhook(projectID int, webhookURL, token string) error {
	pushEvents := true
	mergeRequestsEvents := true
	noteEvents := true
	tagPushEvents := true

	hookOptions := &gitlab.AddProjectHookOptions{
		URL:                 &webhookURL,
		PushEvents:          &pushEvents,
		MergeRequestsEvents: &mergeRequestsEvents,
		NoteEvents:          &noteEvents,
		TagPushEvents:       &tagPushEvents,
		Token:               &token,
	}

	_, _, err := client.Projects.AddProjectHook(projectID, hookOptions)
	if err != nil {
		return fmt.Errorf("failed to add webhook: %w", err)
	}
	return nil
}

func createNewRepository(c *cli.Clients, projectName, targetGroupNamespace, namespace string) error {
	repo := &pacv1alpha1.Repository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "pipelinesascode.tekton.dev/v1alpha1",
			Kind:       "Repository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      projectName,
			Namespace: namespace,
		},
		Spec: pacv1alpha1.RepositorySpec{
			URL: fmt.Sprintf("https://gitlab.com/%s/%s", targetGroupNamespace, projectName),
			GitProvider: &pacv1alpha1.GitProvider{
				URL: "https://gitlab.com",
				Secret: &pacv1alpha1.Secret{
					Name: webhookConfigName,
					Key:  "provider.token",
				},
				WebhookSecret: &pacv1alpha1.Secret{
					Name: webhookConfigName,
					Key:  "webhook.secret",
				},
			},
		},
	}

	repo, err := c.PacClientset.Repositories(namespace).Create(context.Background(), repo, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create repository: %v", err)
	}

	log.Printf("Repository '%s' created successfully in namespace '%s'", repo.GetName(), repo.GetNamespace())
	return nil
}

func GeneratePipelineRunYaml(event, branch string) {
	projectIDStr := store.GetScenarioData("projectID")
	if projectIDStr == nil {
		Fail("projectID not found in store")
	}
	projectID, err := strconv.Atoi(projectIDStr.(string))
	if err != nil {
		Fail(fmt.Sprintf("Failed to convert projectID to int: %v", err))
	}

	yamlContent := `
apiVersion: tekton.dev/v1
kind: PipelineRun
metadata:
  generateName: pr-pipelinerun-
spec:
  pipelineSpec:
    tasks:
      - name: hello-world-pr
        taskSpec:
          steps:
            - name: echo
              image: alpine
              script: |
                #!/bin/sh
                echo "Hello from a pull request!"
`
	filePath := ".tekton/pipelinerun.yaml"
	_, _, err = client.RepositoryFiles.CreateFile(projectID, filePath, &gitlab.CreateFileOptions{
		Branch:        gitlab.String("main"),
		Content:       gitlab.String(yamlContent),
		CommitMessage: gitlab.String("Add pipelinerun for PaC"),
	})

	// If the file already exists, try to update it.
	if err != nil {
		if strings.Contains(err.Error(), "A file with this name already exists") {
			log.Printf("File %s already exists, updating it.", filePath)
			_, _, err = client.RepositoryFiles.UpdateFile(projectID, filePath, &gitlab.UpdateFileOptions{
				Branch:        gitlab.String("main"),
				Content:       gitlab.String(yamlContent),
				CommitMessage: gitlab.String("Update pipelinerun for PaC"),
			})
			if err != nil {
				Fail(fmt.Sprintf("Failed to update file %s: %v", filePath, err))
			}
		} else {
			Fail(fmt.Sprintf("Failed to create file %s: %v", filePath, err))
		}
	}
}

func ConfigurePreviewChanges() {
	projectIDStr := store.GetScenarioData("projectID")
	if projectIDStr == nil {
		Fail("projectID not found in store")
	}
	projectID, err := strconv.Atoi(projectIDStr.(string))
	if err != nil {
		Fail(fmt.Sprintf("Failed to convert projectID to int: %v", err))
	}

	// Create a new branch
	branchName := fmt.Sprintf("test-branch-%d", time.Now().UnixNano())
	_, _, err = client.Branches.CreateBranch(projectID, &gitlab.CreateBranchOptions{
		Branch: gitlab.String(branchName),
		Ref:    gitlab.String("main"),
	})
	if err != nil {
		Fail(fmt.Sprintf("Failed to create branch: %v", err))
	}
	store.PutScenarioData("branchName", branchName)

	// Make a change in the new branch by creating a new file
	filePath := fmt.Sprintf("test-file-%d.txt", time.Now().UnixNano())
	_, _, err = client.RepositoryFiles.CreateFile(projectID, filePath, &gitlab.CreateFileOptions{
		Branch:        gitlab.String(branchName),
		Content:       gitlab.String("hello world"),
		CommitMessage: gitlab.String("Test commit for MR"),
	})
	if err != nil {
		Fail(fmt.Sprintf("Failed to create file: %v", err))
	}

	// Create a merge request
	mr, _, err := client.MergeRequests.CreateMergeRequest(projectID, &gitlab.CreateMergeRequestOptions{
		SourceBranch: gitlab.String(branchName),
		TargetBranch: gitlab.String("main"),
		Title:        gitlab.String("Test MR"),
	})
	if err != nil {
		Fail(fmt.Sprintf("Failed to create merge request: %v", err))
	}
	store.PutScenarioData("mrIID", mr.IID)
}

func GetPipelineNameFromMR() string {
	c := store.Clients()
	namespace := store.Namespace()
	mrIID := store.GetScenarioData("mrIID").(int)
	projectName := store.GetScenarioData("projectName").(string)

	var prName string
	err := wait.PollImmediate(10*time.Second, 5*time.Minute, func() (bool, error) {
		prList, err := c.Tekton.TektonV1().PipelineRuns(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("pipelinesascode.tekton.dev/repository=%s,pipelinesascode.tekton.dev/event-type=pull_request", projectName),
		})
		if err != nil {
			log.Printf("Error listing pipelineruns: %v. Retrying...", err)
			return false, nil
		}

		for _, pr := range prList.Items {
			if pr.Annotations["pipelinesascode.tekton.dev/pull-request"] == strconv.Itoa(mrIID) {
				prName = pr.Name
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		Fail(fmt.Sprintf("Could not find PipelineRun for MR %d in repo %s: %v", mrIID, projectName, err))
	}
	Expect(prName).NotTo(BeEmpty(), "PipelineRun name should not be empty")
	return prName
}

func CleanupPAC(c *cli.Clients, smeeDeploymentName, namespace string) {
	// Remove the generated PipelineRun YAML file
	fileName := store.GetScenarioData("fileName")
	if fileName != nil && fileName.(string) != "" {
		if err := os.Remove(fileName.(string)); err != nil {
			Fail(fmt.Sprintf("failed to remove file %s: %v", fileName.(string), err))
		}
	}

	// Remove Forked Project
	projectIDStr := store.GetScenarioData("projectID")
	if projectIDStr != nil && projectIDStr.(string) != "" {
		projectID, err := strconv.Atoi(projectIDStr.(string))
		if err != nil {
			Fail(fmt.Sprintf("failed to convert project ID to integer: %v", err))
		}
		if cleanupErr := deleteGitlabProject(projectID); cleanupErr != nil {
			Fail(fmt.Sprintf("cleanup failed: %v", cleanupErr))
		}
	}

	// Delete Smee Deployment
	if smeeDeploymentName != "" {
		if err := k8s.DeleteDeployment(c, namespace, smeeDeploymentName); err != nil {
			if errors.IsNotFound(err) {
				log.Printf("Deployment %q not found in namespace %q, skipping deletion.", smeeDeploymentName, namespace)
			} else {
				Fail(fmt.Sprintf("failed to Delete Smee Deployment: %v", err))
			}
		}
	} else {
		log.Printf("Smee deployment name is empty, skipping deployment deletion.")
	}

	// Delete webhook secret (retained from existing CleanupPAC)
	if namespace != "" {
		kc := c.KubeClient.Kube
		if err := kc.CoreV1().Secrets(namespace).Delete(context.TODO(), webhookConfigName, metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				log.Printf("Secret %q not found in namespace %q, skipping deletion.", webhookConfigName, namespace)
			} else {
				Fail(fmt.Sprintf("failed to delete secret %s: %v", webhookConfigName, err))
			}
		}
	}
}

func deleteGitlabProject(projectID int) error {
	_, err := client.Projects.DeleteProject(projectID)
	if err != nil {
		// Check if the error message indicates the project is already marked for deletion
		if strings.Contains(err.Error(), "Project has been already marked for deletion") {
			log.Printf("Project ID %d is already marked for deletion, skipping further deletion attempts.", projectID)
			return nil // Consider it a success, as the project is being deleted
		}
		return fmt.Errorf("failed to delete project: %w", err)
	}
	log.Println("Project successfully deleted.")
	return nil
}
