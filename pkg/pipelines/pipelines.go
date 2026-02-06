package pipelines

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/clients"
	"github.com/srivickynesh/release-tests-ginkgo/pkg/opc"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ValidatePipelineRun(clients *clients.Clients, name, expectedStatus, check, namespace string) {
	var prs []opc.PipelineRunList
	var lastStatus string

	err := wait.PollUntilContextTimeout(context.Background(), time.Second*30, time.Minute*15, true, func(ctx context.Context) (bool, error) {
		var err error
		prs, err = opc.GetOpcPrList(name, namespace)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				log.Printf("pipelinerun %s not found, waiting...", name)
				return false, nil // Keep trying
			}
			return false, err // Real error
		}

		for _, pr := range prs {
			if strings.HasPrefix(pr.Name, name) {
				lastStatus = pr.Status
				if pr.Status == "Succeeded" || pr.Status == "Failed" {
					return true, nil
				}
				// Keep polling if it's running or in another state
				return false, nil
			}
		}
		return false, nil // Pipelinerun not found yet
	})

	if err != nil {
		Fail(fmt.Sprintf("Timeout waiting for PipelineRun %s to complete. Last seen status: %s. Error: %v", name, lastStatus, err))
	}

	found := false
	for _, pr := range prs {
		if strings.HasPrefix(pr.Name, name) {
			var mappedStatus string
			switch expectedStatus {
			case "success":
				mappedStatus = "Succeeded"
			case "fail":
				mappedStatus = "Failed"
			default:
				mappedStatus = expectedStatus
			}
			Expect(pr.Status).To(Equal(mappedStatus), "PipelineRun %s has status %s, but expected %s", pr.Name, pr.Status, mappedStatus)
			found = true
			break
		}
	}
	Expect(found).To(BeTrue(), "PipelineRun with prefix %s not found after completion", name)
}
