package config

import "time"

const (
	// APIRetry defines the frequency at which we check for updates against the
	// k8s api when waiting for a specific condition to be true.
	APIRetry = time.Second * 5

	CLITimeout = 5 * time.Minute

	// TargetNamespace specify the name of Target namespace
	TargetNamespace = "openshift-pipelines"

	// Name of console deployment
	ConsolePluginDeployment = "pipelines-console-plugin"
)

func Path(path string) string {
	return path
}
