#!/usr/bin/env bash
set -euo pipefail

# run-tests.sh -- Standardized Ginkgo test invocation wrapper
#
# Usage:
#   ./scripts/run-tests.sh [MODE] [OPTIONS]
#
# Modes:
#   sanity          Run sanity-labeled tests
#   smoke           Run smoke-labeled tests
#   e2e             Run e2e-labeled tests
#   e2e-connected   Run e2e tests excluding disconnected
#   disconnected    Run disconnected-labeled tests
#   all             Run all tests (no label filter)
#   <custom>        Any other string is passed as --label-filter value
#
# Options:
#   --ci            Enable CI mode (--fail-on-focused, --no-color)
#   --parallel, -p  Enable parallel execution (default 4 procs)
#   --path=<path>   Override test path (default: ./tests/...)
#   --help, -h      Show this help message
#
# Environment variables:
#   CI=true             Automatically enables CI mode
#   ARTIFACTS_DIR       Output directory for reports (default: ./artifacts)
#   GINKGO_TIMEOUT      Test timeout (default: 4h)
#   GINKGO_PROCS        Number of parallel processes (default: 4)

usage() {
    sed -n '3,28p' "$0" | sed 's/^# \?//'
    exit 0
}

# Defaults
MODE="all"
CI_MODE="${CI:-false}"
PARALLEL=false
TEST_PATH="./tests/..."
ARTIFACTS="${ARTIFACTS_DIR:-./artifacts}"
TIMEOUT="${GINKGO_TIMEOUT:-4h}"
PROCS="${GINKGO_PROCS:-4}"

# Parse arguments
POSITIONAL_ARGS=()
while [[ $# -gt 0 ]]; do
    case "$1" in
        --help|-h)
            usage
            ;;
        --ci)
            CI_MODE="true"
            shift
            ;;
        --parallel|-p)
            PARALLEL=true
            shift
            ;;
        --path=*)
            TEST_PATH="${1#*=}"
            shift
            ;;
        -*)
            echo "Unknown option: $1" >&2
            echo "Run with --help for usage information." >&2
            exit 1
            ;;
        *)
            POSITIONAL_ARGS+=("$1")
            shift
            ;;
    esac
done

# First positional arg is the mode
if [[ ${#POSITIONAL_ARGS[@]} -gt 0 ]]; then
    MODE="${POSITIONAL_ARGS[0]}"
fi

# Remaining positional args override test path
if [[ ${#POSITIONAL_ARGS[@]} -gt 1 ]]; then
    TEST_PATH="${POSITIONAL_ARGS[@]:1}"
fi

# Map mode to label filter
LABEL_FILTER=""
case "$MODE" in
    sanity)
        LABEL_FILTER="sanity"
        ;;
    smoke)
        LABEL_FILTER="smoke"
        ;;
    e2e)
        LABEL_FILTER="e2e"
        ;;
    e2e-connected)
        LABEL_FILTER="e2e && !disconnected"
        ;;
    disconnected)
        LABEL_FILTER="disconnected"
        ;;
    all)
        LABEL_FILTER=""
        ;;
    *)
        # Custom label filter string
        LABEL_FILTER="$MODE"
        ;;
esac

# Create artifacts directory
mkdir -p "$ARTIFACTS"

# Build ginkgo command
GINKGO_ARGS=(
    "ginkgo" "run"
    "--junit-report=junit-report.xml"
    "--output-dir=${ARTIFACTS}"
    "--timeout=${TIMEOUT}"
    "--randomize-all"
    "-v"
)

# Add label filter if set
if [[ -n "$LABEL_FILTER" ]]; then
    GINKGO_ARGS+=("--label-filter=${LABEL_FILTER}")
fi

# CI mode flags
if [[ "$CI_MODE" == "true" ]]; then
    GINKGO_ARGS+=("--fail-on-focused" "--no-color")
fi

# Parallel mode flags
if [[ "$PARALLEL" == "true" ]]; then
    GINKGO_ARGS+=("--procs=${PROCS}")
fi

# Add test path as last argument
GINKGO_ARGS+=("$TEST_PATH")

# Print the command for CI log transparency
echo "==> Executing: ${GINKGO_ARGS[*]}"
echo ""

# Execute
exec "${GINKGO_ARGS[@]}"
