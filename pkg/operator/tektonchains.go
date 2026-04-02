/*
Copyright 2020 The Tekton Authors

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

package operator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/cmd"
	"github.com/openshift-pipelines/release-tests-ginkgo/pkg/config"
	"github.com/tektoncd/operator/pkg/apis/operator/v1alpha1"
	chainv1alpha "github.com/tektoncd/operator/pkg/client/clientset/versioned/typed/operator/v1alpha1"
	"github.com/tektoncd/operator/test/utils"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// "quay.io/openshift-pipeline/chainstest"
var repo string = os.Getenv("CHAINS_REPOSITORY")
var publicKeyPath = config.Path("testdata/chains/key")

func EnsureTektonChainsExists(clients chainv1alpha.TektonChainInterface, names utils.ResourceNames) (*v1alpha1.TektonChain, error) {
	ks, err := clients.Get(context.TODO(), names.TektonChain, metav1.GetOptions{})
	err = wait.PollUntilContextTimeout(context.TODO(), config.APIRetry, config.APITimeout, false, func(context.Context) (bool, error) {
		ks, err = clients.Get(context.TODO(), names.TektonChain, metav1.GetOptions{})
		if err != nil {
			if apierrs.IsNotFound(err) {
				log.Printf("Waiting for availability of chains cr [%s]\n", names.TektonChain)
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return ks, err
}

// UpdateTektonConfigForChains patches the TektonConfig CR to configure Tekton Chains
// with the given format, taskrun storage, OCI storage, and transparency settings.
func UpdateTektonConfigForChains(format, taskrunStorage, ociStorage, transparency string) {
	patchData := fmt.Sprintf(`{"spec":{"chain":{"options":{"disabled":false},"chain-config":{"artifacts.taskrun.format":"%s","artifacts.taskrun.storage":"%s","artifacts.oci.storage":"%s","transparency.enabled":"%s"}}}}`,
		format, taskrunStorage, ociStorage, transparency)
	cmd.MustSucceed("oc", "patch", "tektonconfig", "config", "-p", patchData, "--type=merge")
	log.Printf("Updated TektonConfig for chains: format=%s, taskrunStorage=%s, ociStorage=%s, transparency=%s", format, taskrunStorage, ociStorage, transparency)
}

// RestoreTektonConfigChains restores the TektonConfig chains settings to defaults.
func RestoreTektonConfigChains() {
	patchData := `{"spec":{"chain":{"options":{"disabled":false},"chain-config":{"artifacts.taskrun.format":"in-toto","artifacts.taskrun.storage":"tekton","artifacts.oci.storage":"","transparency.enabled":"false"}}}}`
	cmd.MustSucceed("oc", "patch", "tektonconfig", "config", "-p", patchData, "--type=merge")
	log.Println("Restored TektonConfig chains settings to defaults")
}

func VerifySignature(resourceType string) error {
	// Get a signature of taskrun payload
	resourceUID := cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", "jsonpath='{.metadata.uid}'").Stdout()
	resourceUID = strings.Trim(resourceUID, "'")
	jsonpath := fmt.Sprintf("jsonpath=\"{.metadata.annotations.chains\\.tekton\\.dev/signature-%s-%s}\"", resourceType, resourceUID)
	log.Println("Waiting 30 seconds")
	cmd.MustSucceedIncreasedTimeout(time.Second*45, "sleep", "30")
	signature := cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", jsonpath).Stdout()
	signature = strings.Trim(signature, "\"")

	jsonpath = "jsonpath=\"{.metadata.annotations.chains\\.tekton\\.dev/signed}\""
	isSigned := cmd.MustSucceed("opc", resourceType, "describe", "--last", "-o", jsonpath).Stdout()
	isSigned = strings.Trim(isSigned, "\"")

	if isSigned != "true" {
		return fmt.Errorf("annotation chains.tekton.dev/signed is set to %s", isSigned)
	}
	if len(signature) == 0 {
		return fmt.Errorf("annotation chains.tekton.dev/signature-%s-%s is not set", resourceType, resourceUID)
	}

	// Decode the signature
	decodedSignature, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("error decoding base64: %w", err)
	}
	// Create file with signature
	file, err := os.Create("sign")
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}
	//nolint:errcheck
	defer file.Close()
	_, err = file.WriteString(string(decodedSignature))
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err)
	}
	// Verify signature with signing-secrets
	cmd.MustSucceed("cosign", "verify-blob-attestation", "--insecure-ignore-tlog", "--key", publicKeyPath+"/cosign.pub", "--signature", "sign", "--type", "slsaprovenance", "--check-claims=false", "/dev/null")
	return nil
}

func StartKanikoTask() {
	var tag = time.Now().Format("060102150405")
	cmd.MustSucceed("oc", "secrets", "link", "pipeline", "chains-image-registry-credentials", "--for=pull,mount")
	image := fmt.Sprintf("IMAGE=%s:%s", repo, tag)
	cmd.MustSucceed("opc", "task", "start", "--param", image, "--use-param-defaults", "--workspace", "name=source,claimName=chains-pvc", "--workspace", "name=dockerconfig,secret=chains-image-registry-credentials", "kaniko-chains")
	log.Println("Waiting 2 minutes for images to appear in image registry")
	cmd.MustSucceedIncreasedTimeout(time.Second*130, "sleep", "120")
}

func GetImageUrlAndDigest() (string, string, error) {
	// Get Image digest
	var imageDigest string
	jsonOutput := cmd.MustSucceed("opc", "tr", "describe", "--last", "-o", "jsonpath={.status.results}").Stdout()
	// Parse Json Output
	type Result struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	var results []Result
	err := json.Unmarshal([]byte(jsonOutput), &results)
	if err != nil {
		return "", "", fmt.Errorf("error parsing JSON output: %w", err)
	}

	// Get IMAGE_DIGEST value
	for _, result := range results {
		if strings.Contains(result.Name, "IMAGE_DIGEST") {
			imageDigest = strings.Split(result.Value, ":")[1]
		}
	}

	// Return image url with digest
	url := fmt.Sprintf("%s@sha256:%s", repo, imageDigest)
	return url, imageDigest, nil
}

func VerifyImageSignature() error {
	url, _, err := GetImageUrlAndDigest()
	if err != nil {
		return err
	}
	cmd.MustSucceed("cosign", "verify", "--key", publicKeyPath+"/cosign.pub", url)
	return nil
}

func VerifyAttestation() error {
	url, _, err := GetImageUrlAndDigest()
	if err != nil {
		return err
	}
	cmd.MustSucceed("cosign", "verify-attestation", "--key", publicKeyPath+"/cosign.pub", "--type", "slsaprovenance", url)
	return nil
}

func CheckAttestationExists() error {
	// Get UUID
	_, imageDigest, err := GetImageUrlAndDigest()
	if err != nil {
		return err
	}
	jsonOutput := cmd.MustSucceed("rekor-cli", "search", "--format", "json", "--sha", imageDigest).Stdout()

	// Parse Json output to find UUID
	type UUID struct {
		UUIDs []string `json:"UUIDs"`
	}
	var uuid UUID
	err = json.Unmarshal([]byte(jsonOutput), &uuid)
	if err != nil {
		return fmt.Errorf("error parsing JSON output: %w", err)
	}
	rekorUUID := uuid.UUIDs[0]

	// Check the Attestation
	if strings.Contains(cmd.Run("rekor-cli", "get", "--uuid", rekorUUID).Stdout(), "getLogEntryByUuidNotFound") {
		return fmt.Errorf("failed to find attestation for UUID %s", rekorUUID)
	}
	return nil
}

func CreateFileWithCosignPubKey() error {
	chainsPublicKey := cmd.MustSucceed("oc", "get", "secrets", "signing-secrets", "-n", "openshift-pipelines", "-o", "jsonpath='{.data.cosign\\.pub}'").Stdout()
	chainsPublicKey = strings.Trim(chainsPublicKey, "'")
	decodedPublicKey, err := base64.StdEncoding.DecodeString(chainsPublicKey)
	cmd.MustSucceed("mkdir", "-p", publicKeyPath)
	if err != nil {
		return fmt.Errorf("error decoding base64: %w", err)
	}
	fullPath := filepath.Join(publicKeyPath, "cosign.pub")
	file, err := os.Create(filepath.Clean(fullPath))
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}
	//nolint:errcheck
	defer file.Close()
	_, err = file.WriteString(string(decodedPublicKey))
	if err != nil {
		return fmt.Errorf("error writing to file: %w", err)
	}
	return nil
}
