package tlsscanner

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/clients"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
)

// RunScannerJob creates the tls-scanner Kubernetes Job targeting nsFilter,
// waits until the scanner binary finishes writing results to disk
// (detected via ScanCompleteMarker in pod logs), and returns the pod name.
// The pod remains alive for artifactWaitSecs to allow ExtractResults to run.
func RunScannerJob(cs *clients.Clients, nsFilter string) string {
	deleteExistingJob(cs)

	job := buildScannerJob(nsFilter)
	// Brief pause to allow SCC/RBAC grants to propagate before the pod is scheduled.
	time.Sleep(10 * time.Second)

	log.Printf("Creating tls-scanner Job %q in namespace %q", ScannerJobName, ScannerNamespace)
	_, err := cs.KubeClient.Kube.BatchV1().Jobs(ScannerNamespace).Create(
		context.TODO(), job, metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred(), "failed to create tls-scanner Job")

	podName := waitForScannerPod(cs)
	log.Printf("Scanner pod %q is running; waiting for scan to complete", podName)
	waitForScanComplete(podName)
	log.Printf("Scan complete — results are ready in pod %q:%s", podName, resultsDir)
	return podName
}

// ExtractResults copies results.json from the scanner pod to destPath using
// oc cp. Must be called while the pod is still alive (within artifactWaitSecs).
func ExtractResults(podName, destPath string) {
	// src already embeds the namespace ("namespace/pod:/path"), so do NOT also
	// pass -n: combining both causes oc cp to misparse the pod reference and fail.
	src := fmt.Sprintf("%s/%s:%s/results.json", ScannerNamespace, podName, resultsDir)
	log.Printf("Extracting scan results: oc cp %s %s", src, destPath)
	cmd.MustSucceedIncreasedTimeout(2*time.Minute,
		"oc", "cp", src, destPath)
}

// ── internal helpers ──────────────────────────────────────────────────────────

func buildScannerJob(nsFilter string) *batchv1.Job {
	backoff := int32(2)
	privileged := true

	// Shell command mirrors the upstream scanner-job.yaml.template exactly:
	// run scanner in background, tail -f the log to stdout, wait for scanner,
	// then sleep for artifact collection before always exiting 0.
	shellCmd := fmt.Sprintf(`
/usr/local/bin/tls-scanner --all-pods -j 4 \
  --artifact-dir %s \
  --json-file %s/results.json \
  --csv-file %s/results.csv \
  --timing-file timing.txt \
  --log-file %s/scan.log \
  --namespace-filter %s &

SCANNER_PID=$!
tail -f --retry %s/scan.log &
TAIL_PID=$!

wait $SCANNER_PID
exit_code=$?

kill $TAIL_PID 2>/dev/null || true
sleep 2

echo ""
echo "=========================================="
echo "Scanner finished with exit code: $exit_code"
echo "=========================================="

echo "%s"
sleep %s
exit 0
`,
		resultsDir, resultsDir, resultsDir, resultsDir,
		nsFilter,
		resultsDir,
		ScanCompleteMarker,
		artifactWaitSecs,
	)

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ScannerJobName,
			Namespace: ScannerNamespace,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: &backoff,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: scannerSA,
					RestartPolicy:      corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "tls-scanner",
							Image:   ScannerImage,
							Command: []string{"/bin/sh", "-c"},
							Args:    []string{shellCmd},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privileged,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "artifacts", MountPath: resultsDir},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "artifacts",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}
}

// deleteExistingJob removes a leftover scanner Job from a previous run.
func deleteExistingJob(cs *clients.Clients) {
	propagation := metav1.DeletePropagationBackground
	_ = cs.KubeClient.Kube.BatchV1().Jobs(ScannerNamespace).Delete(
		context.TODO(), ScannerJobName,
		metav1.DeleteOptions{PropagationPolicy: &propagation})
}

// waitForScannerPod blocks until exactly one pod for the scanner Job has been
// assigned a name and is no longer Pending. The scanner binary may run fast
// enough that the pod transitions Pending→Running→Succeeded before the first
// poll, so we accept Running OR Succeeded as valid "active" states and return
// as soon as the pod name is known and the phase is not Pending.
func waitForScannerPod(cs *clients.Clients) string {
	selector := "job-name=" + ScannerJobName
	var podName string
	err := wait.PollUntilContextTimeout(
		context.TODO(), config.APIRetry, 10*time.Minute, true,
		func(context.Context) (bool, error) {
			pods, err := cs.KubeClient.Kube.CoreV1().Pods(ScannerNamespace).List(
				context.TODO(), metav1.ListOptions{LabelSelector: selector})
			if err != nil || len(pods.Items) == 0 {
				log.Printf("Waiting for scanner pod to appear...")
				return false, nil
			}
			pod := pods.Items[0]
			switch pod.Status.Phase {
			case corev1.PodPending:
				log.Printf("Scanner pod %q phase: Pending", pod.Name)
				return false, nil
			case corev1.PodFailed:
				// Log container state before aborting so the failure reason is visible.
				for _, cs := range pod.Status.ContainerStatuses {
					if cs.State.Terminated != nil {
						t := cs.State.Terminated
						log.Printf("Scanner container %q exit: code=%d reason=%q message=%q",
							cs.Name, t.ExitCode, t.Reason, t.Message)
					}
				}
				if len(pod.Status.ContainerStatuses) == 0 {
					log.Printf("Scanner pod %q failed with no container statuses; events may explain (check pod conditions: %+v)", pod.Name, pod.Status.Conditions)
				}
				return false, fmt.Errorf("scanner pod %q entered Failed phase", pod.Name)
			default:
				// Running, Succeeded, or Unknown — pod name is known, proceed.
				podName = pod.Name
				log.Printf("Scanner pod %q phase: %s", pod.Name, pod.Status.Phase)
				return true, nil
			}
		})
	Expect(err).NotTo(HaveOccurred(), "scanner pod did not start")
	return podName
}

// waitForScanComplete polls pod logs until ScanCompleteMarker appears,
// indicating the scanner binary has finished and results are written to disk.
func waitForScanComplete(podName string) {
	err := wait.PollUntilContextTimeout(
		context.TODO(), 15*time.Second, 20*time.Minute, false,
		func(context.Context) (bool, error) {
			result := cmd.Run("oc", "logs", "-n", ScannerNamespace, podName)
			if result.ExitCode == 0 && strings.Contains(result.Stdout(), ScanCompleteMarker) {
				return true, nil
			}
			log.Printf("Waiting for scanner to complete (pod %q)...", podName)
			return false, nil
		})
	Expect(err).NotTo(HaveOccurred(),
		"tls-scanner did not complete within the allowed window")
}
