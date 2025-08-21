package config

import "time"

const CLITimeout = 5 * time.Minute
const TargetNamespace = "openshift-pipelines"

func Path(path string) string {
	return path
}
