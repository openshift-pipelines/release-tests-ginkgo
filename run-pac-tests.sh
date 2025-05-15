#!/bin/bash

# Script to run PAC tests with GitLab configuration

# Set GitLab environment variables

# Optional: Set test namespace (will use GITLAB_GROUP_NS if not set)
# export TEST_NAMESPACE=test2602035

# Optional: Set merge request IID for pipeline validation test
# export GITLAB_MR_IID=1

# Optional: Set Smee URL for webhook forwarding
# export SMEE_URL=https://smee.io/your-webhook-url

# Optional: Set GitLab repository URL
export GITLAB_REPO_URL=https://gitlab.com/test2602035/test2602035

# Optional: Set GitLab webhook URL (required if cluster API is not publicly accessible)
# This should be the publicly accessible URL for the PAC webhook route
# export GITLAB_WEBHOOK_URL=https://your-webhook-route.example.com

echo "Running PAC tests with GitLab configuration..."
echo "GITLAB_GROUP_NS: $GITLAB_GROUP_NS"
echo "GITLAB_PROJECT_ID: $GITLAB_PROJECT_ID"
echo ""

# Run tests using go test
go test -v ./tests/pac/... -ginkgo.v

# Alternative: If ginkgo CLI is installed, use:
# ginkgo -v tests/pac/
