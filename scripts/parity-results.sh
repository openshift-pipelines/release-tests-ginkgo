#!/usr/bin/env bash
#
# parity-results.sh -- Run both Ginkgo and Gauge suites and produce pass/fail delta report
#
# This script orchestrates running both test suites against the same cluster
# state and compares results. It produces a delta report showing matching,
# divergent, and missing test results between the two frameworks.
#
# Usage:
#   ./scripts/parity-results.sh [OPTIONS]
#
# Options:
#   --ginkgo-only     Skip Gauge run, use existing /tmp/gauge-junit.xml
#   --gauge-only      Skip Ginkgo run, use existing /tmp/ginkgo-junit.xml
#   --compare-only    Skip both runs, compare existing JUnit XMLs
#   --label-filter    Label filter for Ginkgo (e.g., "sanity")
#   --gauge-tags      Tags filter for Gauge (e.g., "sanity")
#   --output-dir      Directory for output files (default: /tmp)
#   --help            Show this help message
#
# Prerequisites:
#   - oc CLI logged into an OpenShift cluster
#   - ginkgo CLI available (go install github.com/onsi/ginkgo/v2/ginkgo@latest)
#   - gauge CLI available (for Gauge runs, optional with --ginkgo-only)
#   - xmllint available (for XML parsing, part of libxml2)
#
# Exit codes:
#   0  Zero unjustified divergences
#   1  Divergences found or error
#
set -euo pipefail

# -------------------------------------------------------------------
# Configuration
# -------------------------------------------------------------------

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="/tmp"
LABEL_FILTER=""
GAUGE_TAGS=""
RUN_GINKGO=true
RUN_GAUGE=true
COMPARE_ONLY=false

GINKGO_JUNIT="${OUTPUT_DIR}/ginkgo-junit.xml"
GAUGE_JUNIT="${OUTPUT_DIR}/gauge-junit.xml"
DELTA_REPORT="${OUTPUT_DIR}/parity-delta-report.txt"
DROPPED_TESTS="${PROJECT_ROOT}/.planning/phases/11-parity-validation/DROPPED-TESTS.md"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# -------------------------------------------------------------------
# Parse arguments
# -------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case $1 in
        --ginkgo-only)
            RUN_GAUGE=false
            shift
            ;;
        --gauge-only)
            RUN_GINKGO=false
            shift
            ;;
        --compare-only)
            COMPARE_ONLY=true
            RUN_GINKGO=false
            RUN_GAUGE=false
            shift
            ;;
        --label-filter)
            LABEL_FILTER="$2"
            shift 2
            ;;
        --gauge-tags)
            GAUGE_TAGS="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            GINKGO_JUNIT="${OUTPUT_DIR}/ginkgo-junit.xml"
            GAUGE_JUNIT="${OUTPUT_DIR}/gauge-junit.xml"
            DELTA_REPORT="${OUTPUT_DIR}/parity-delta-report.txt"
            shift 2
            ;;
        --help)
            head -25 "$0" | tail -20
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# -------------------------------------------------------------------
# Prerequisites check
# -------------------------------------------------------------------

check_prerequisites() {
    echo "Checking prerequisites..."
    local missing=false

    if ! command -v oc &> /dev/null; then
        echo -e "  ${RED}MISSING${NC}: oc CLI not found"
        missing=true
    else
        echo -e "  ${GREEN}OK${NC}: oc CLI found"
    fi

    # Verify cluster login
    if command -v oc &> /dev/null; then
        if oc whoami &> /dev/null; then
            echo -e "  ${GREEN}OK${NC}: Logged into cluster as $(oc whoami)"
        else
            echo -e "  ${RED}MISSING${NC}: Not logged into OpenShift cluster"
            missing=true
        fi
    fi

    if $RUN_GINKGO && ! command -v ginkgo &> /dev/null; then
        echo -e "  ${RED}MISSING${NC}: ginkgo CLI not found"
        echo "    Install: go install github.com/onsi/ginkgo/v2/ginkgo@latest"
        missing=true
    elif $RUN_GINKGO; then
        echo -e "  ${GREEN}OK${NC}: ginkgo CLI found"
    fi

    if $RUN_GAUGE && ! command -v gauge &> /dev/null; then
        echo -e "  ${YELLOW}WARNING${NC}: gauge CLI not found"
        echo "    Gauge run will be skipped. Provide gauge-junit.xml with --ginkgo-only"
        RUN_GAUGE=false
    elif $RUN_GAUGE; then
        echo -e "  ${GREEN}OK${NC}: gauge CLI found"
    fi

    if $missing; then
        echo ""
        echo -e "${RED}Prerequisites not met. Fix issues above and retry.${NC}"
        exit 1
    fi

    echo ""
}

# -------------------------------------------------------------------
# Record cluster state
# -------------------------------------------------------------------

record_cluster_state() {
    echo "Recording cluster state..."
    echo ""

    echo "  OpenShift version:"
    oc version --short 2>/dev/null || oc version 2>/dev/null | head -3 || echo "    (unable to determine)"

    echo ""
    echo "  TektonConfig status:"
    oc get tektonconfig config -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "    (unable to determine)"

    echo ""
    echo "  Tekton Pipelines version:"
    oc get tektonconfig config -o jsonpath='{.status.conditions[?(@.type=="Ready")].message}' 2>/dev/null || \
        oc get deployment -n openshift-pipelines tekton-pipelines-controller -o jsonpath='{.metadata.labels.pipeline\.tekton\.dev/release}' 2>/dev/null || \
        echo "    (unable to determine)"

    echo ""
}

# -------------------------------------------------------------------
# Run Gauge suite
# -------------------------------------------------------------------

run_gauge_suite() {
    echo "=============================================="
    echo "  Running Gauge Suite"
    echo "=============================================="
    echo ""

    cd "${PROJECT_ROOT}/reference-repo/release-tests"

    local gauge_args="run --env=default"
    if [ -n "$GAUGE_TAGS" ]; then
        gauge_args="$gauge_args --tags \"$GAUGE_TAGS\""
    fi

    echo "Running: gauge $gauge_args specs/"
    eval gauge "$gauge_args" specs/ 2>&1 || true

    # Collect JUnit XML
    if [ -f "reports/xml-report/result.xml" ]; then
        cp "reports/xml-report/result.xml" "$GAUGE_JUNIT"
        echo ""
        echo -e "${GREEN}Gauge JUnit XML saved to: $GAUGE_JUNIT${NC}"
    else
        echo -e "${RED}ERROR: Gauge JUnit XML not found at reports/xml-report/result.xml${NC}"
        echo "Looking for any XML reports..."
        find reports/ -name "*.xml" -type f 2>/dev/null || echo "  No XML reports found"
    fi

    cd "$PROJECT_ROOT"
    echo ""
}

# -------------------------------------------------------------------
# Reset cluster state between runs
# -------------------------------------------------------------------

reset_cluster_state() {
    echo "=============================================="
    echo "  Resetting Cluster State"
    echo "=============================================="
    echo ""

    echo "Deleting test namespaces..."
    oc delete ns -l created-by=release-tests --wait=false 2>/dev/null || true
    oc delete ns -l created-by=ginkgo-tests --wait=false 2>/dev/null || true

    echo "Waiting for TektonConfig to reconcile..."
    oc wait tektonconfig/config --for=condition=Ready --timeout=300s 2>/dev/null || \
        echo "  WARNING: TektonConfig wait timed out or not available"

    echo "Cluster state reset complete."
    echo ""
}

# -------------------------------------------------------------------
# Run Ginkgo suite
# -------------------------------------------------------------------

run_ginkgo_suite() {
    echo "=============================================="
    echo "  Running Ginkgo Suite"
    echo "=============================================="
    echo ""

    cd "$PROJECT_ROOT"

    local ginkgo_args="run --junit-report=ginkgo-junit.xml"
    if [ -n "$LABEL_FILTER" ]; then
        ginkgo_args="$ginkgo_args --label-filter=\"$LABEL_FILTER\""
    fi

    echo "Running: ginkgo $ginkgo_args ./tests/..."
    eval ginkgo "$ginkgo_args" ./tests/... 2>&1 || true

    # Collect JUnit XML
    if [ -f "ginkgo-junit.xml" ]; then
        cp "ginkgo-junit.xml" "$GINKGO_JUNIT"
        echo ""
        echo -e "${GREEN}Ginkgo JUnit XML saved to: $GINKGO_JUNIT${NC}"
    else
        echo -e "${RED}ERROR: Ginkgo JUnit XML not found${NC}"
        echo "Looking for any JUnit XMLs..."
        find . -maxdepth 2 -name "*.xml" -type f 2>/dev/null || echo "  No XML files found"
    fi

    echo ""
}

# -------------------------------------------------------------------
# Extract test results from JUnit XML
#
# Parses JUnit XML to produce sorted list: POLARION_ID STATUS
# where STATUS is PASS, FAIL, SKIP, or ERROR
#
# Arguments:
#   $1  Path to JUnit XML file
# -------------------------------------------------------------------

extract_results() {
    local junit_file="$1"
    local results=""

    if [ ! -f "$junit_file" ]; then
        echo ""
        return
    fi

    # Try xmllint first (most reliable)
    if command -v xmllint &> /dev/null; then
        # Extract testcase elements
        local testcases
        testcases=$(xmllint --xpath '//testcase' "$junit_file" 2>/dev/null || true)

        if [ -n "$testcases" ]; then
            # Parse each testcase for name and status
            while IFS= read -r line; do
                local name
                name=$(echo "$line" | grep -o 'name="[^"]*"' | head -1 | sed 's/name="//;s/"$//' || true)
                local id
                id=$(echo "$name" | grep -o 'PIPELINES-[0-9]*-TC[0-9]*' || true)

                if [ -z "$id" ]; then
                    continue
                fi

                # Check for failure/error/skipped elements
                if echo "$line" | grep -q '<failure'; then
                    results="$results$id FAIL\n"
                elif echo "$line" | grep -q '<error'; then
                    results="$results$id ERROR\n"
                elif echo "$line" | grep -q '<skipped'; then
                    results="$results$id SKIP\n"
                else
                    results="$results$id PASS\n"
                fi
            done < <(echo "$testcases" | tr '>' '\n' | grep 'testcase ')
        fi
    fi

    # Fallback: awk-based parsing if xmllint didn't produce results
    if [ -z "$results" ]; then
        while IFS= read -r line; do
            if echo "$line" | grep -q '<testcase '; then
                local name
                name=$(echo "$line" | grep -o 'name="[^"]*"' | head -1 | sed 's/name="//;s/"$//')
                local id
                id=$(echo "$name" | grep -o 'PIPELINES-[0-9]*-TC[0-9]*' || true)

                if [ -z "$id" ]; then
                    continue
                fi

                # Read ahead for failure/error/skipped
                local status="PASS"
                # Simple heuristic: check if same line or next lines have failure indicators
                if echo "$line" | grep -q 'failure\|<failure'; then
                    status="FAIL"
                elif echo "$line" | grep -q 'error\|<error'; then
                    status="ERROR"
                elif echo "$line" | grep -q 'skipped\|<skipped'; then
                    status="SKIP"
                fi

                results="$results$id $status\n"
            fi
        done < "$junit_file"
    fi

    # Deduplicate and sort
    echo -e "$results" | sort -u | grep -v '^$' || true
}

# -------------------------------------------------------------------
# Compare results and produce delta report
# -------------------------------------------------------------------

compare_results() {
    echo "=============================================="
    echo "  Pass/Fail Delta Report"
    echo "=============================================="
    echo ""

    # Check input files
    local has_ginkgo=false
    local has_gauge=false

    if [ -f "$GINKGO_JUNIT" ]; then
        has_ginkgo=true
        echo "Ginkgo JUnit: $GINKGO_JUNIT ($(wc -c < "$GINKGO_JUNIT" | tr -d ' ') bytes)"
    else
        echo -e "${YELLOW}Ginkgo JUnit XML not available${NC}"
    fi

    if [ -f "$GAUGE_JUNIT" ]; then
        has_gauge=true
        echo "Gauge JUnit:  $GAUGE_JUNIT ($(wc -c < "$GAUGE_JUNIT" | tr -d ' ') bytes)"
    else
        echo -e "${YELLOW}Gauge JUnit XML not available${NC}"
    fi

    echo ""

    if ! $has_ginkgo && ! $has_gauge; then
        echo -e "${YELLOW}No JUnit XMLs available for comparison.${NC}"
        echo "Run the suites first, or provide XML files at:"
        echo "  $GINKGO_JUNIT"
        echo "  $GAUGE_JUNIT"
        echo ""
        echo "Generating template delta report..."
        generate_template_report
        return
    fi

    # Extract results
    local ginkgo_results=""
    local gauge_results=""

    if $has_ginkgo; then
        ginkgo_results=$(extract_results "$GINKGO_JUNIT")
        local ginkgo_count
        ginkgo_count=$(echo "$ginkgo_results" | grep -c '.' || echo "0")
        echo "Ginkgo test results extracted: $ginkgo_count"
    fi

    if $has_gauge; then
        gauge_results=$(extract_results "$GAUGE_JUNIT")
        local gauge_count
        gauge_count=$(echo "$gauge_results" | grep -c '.' || echo "0")
        echo "Gauge test results extracted: $gauge_count"
    fi

    echo ""

    # Load dropped tests for filtering
    local dropped_ids=""
    if [ -f "$DROPPED_TESTS" ]; then
        dropped_ids=$(grep -o 'PIPELINES-[0-9]*-TC[0-9]*' "$DROPPED_TESTS" | sort -u || true)
    fi

    # If both have results, compare
    if [ -n "$ginkgo_results" ] && [ -n "$gauge_results" ]; then
        # Get unique IDs from both
        local ginkgo_ids
        ginkgo_ids=$(echo "$ginkgo_results" | awk '{print $1}' | sort -u)
        local gauge_ids
        gauge_ids=$(echo "$gauge_results" | awk '{print $1}' | sort -u)

        # Categories
        local matching=0
        local divergent=0
        local ginkgo_missing=0
        local gauge_missing=0
        local divergent_details=""

        # Check each Gauge result against Ginkgo
        while IFS= read -r gauge_line; do
            local id status
            id=$(echo "$gauge_line" | awk '{print $1}')
            status=$(echo "$gauge_line" | awk '{print $2}')

            # Check if this ID was dropped
            if echo "$dropped_ids" | grep -q "^${id}$"; then
                continue
            fi

            local ginkgo_status
            ginkgo_status=$(echo "$ginkgo_results" | grep "^${id} " | awk '{print $2}' || true)

            if [ -z "$ginkgo_status" ]; then
                ginkgo_missing=$((ginkgo_missing + 1))
            elif [ "$ginkgo_status" = "$status" ]; then
                matching=$((matching + 1))
            else
                divergent=$((divergent + 1))
                divergent_details="$divergent_details  $id: Gauge=$status Ginkgo=$ginkgo_status\n"
            fi
        done <<< "$gauge_results"

        # Check for Ginkgo-only results
        while IFS= read -r ginkgo_line; do
            local id
            id=$(echo "$ginkgo_line" | awk '{print $1}')
            local gauge_status
            gauge_status=$(echo "$gauge_results" | grep "^${id} " | awk '{print $2}' || true)
            if [ -z "$gauge_status" ]; then
                gauge_missing=$((gauge_missing + 1))
            fi
        done <<< "$ginkgo_results"

        local total=$((matching + divergent + ginkgo_missing))

        echo "Results Comparison:"
        echo "  Matching results: $matching"
        echo "  Divergent results: $divergent"
        echo "  Missing in Ginkgo: $ginkgo_missing"
        echo "  Extra in Ginkgo: $gauge_missing"
        echo ""

        if [ "$divergent" -gt 0 ]; then
            echo "Divergent test details:"
            echo -e "$divergent_details"
        fi

        # Write delta report
        {
            echo "Parity Delta Report"
            echo "==================="
            echo "Date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
            echo ""
            echo "Total compared: $total"
            echo "Matching: $matching"
            echo "Divergent: $divergent"
            echo "Missing in Ginkgo: $ginkgo_missing"
            echo "Extra in Ginkgo: $gauge_missing"
            echo ""
            if [ "$divergent" -gt 0 ]; then
                echo "Divergent Details:"
                echo -e "$divergent_details"
            fi
        } > "$DELTA_REPORT"

        echo "Delta report saved to: $DELTA_REPORT"
        echo ""

        if [ "$divergent" -eq 0 ]; then
            echo -e "${GREEN}PASS${NC}: Zero divergences in pass/fail results"
            return 0
        else
            echo -e "${RED}FAIL${NC}: $divergent divergent results found"
            return 1
        fi
    else
        generate_template_report
    fi
}

# -------------------------------------------------------------------
# Generate template delta report when no JUnit XMLs are available
# -------------------------------------------------------------------

generate_template_report() {
    echo "Generating template delta report (no runtime data available)..."
    echo ""

    # Count expected tests from Polarion IDs
    local ginkgo_id_count
    ginkgo_id_count=$(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' "$PROJECT_ROOT/tests/" --include="*_test.go" | sort -u | wc -l | tr -d ' ')

    local gauge_id_count
    gauge_id_count=$(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' "$PROJECT_ROOT/reference-repo/release-tests/specs/" | sort -u | wc -l | tr -d ' ')

    {
        echo "Parity Delta Report (Template)"
        echo "==============================="
        echo "Date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
        echo "Status: PENDING CLUSTER VALIDATION"
        echo ""
        echo "Expected test counts:"
        echo "  Gauge Polarion IDs: $gauge_id_count"
        echo "  Ginkgo Polarion IDs: $ginkgo_id_count"
        echo "  Expected delta: $((gauge_id_count - ginkgo_id_count)) (documented in DROPPED-TESTS.md)"
        echo ""
        echo "To run full comparison:"
        echo "  1. Log into OpenShift cluster: oc login ..."
        echo "  2. Run: ./scripts/parity-results.sh"
        echo "  3. Review delta report at: $DELTA_REPORT"
        echo ""
        echo "For partial validation:"
        echo "  ./scripts/parity-results.sh --label-filter sanity --gauge-tags sanity"
    } > "$DELTA_REPORT"

    echo "Template delta report saved to: $DELTA_REPORT"
    echo ""
    echo -e "${YELLOW}PENDING${NC}: Cluster validation required for pass/fail comparison"
}

# -------------------------------------------------------------------
# Main execution
# -------------------------------------------------------------------

main() {
    echo "=============================================="
    echo "  Parity Results Validation"
    echo "=============================================="
    echo ""
    echo "Project root: $PROJECT_ROOT"
    echo "Output dir: $OUTPUT_DIR"
    echo "Date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    echo ""

    if ! $COMPARE_ONLY; then
        check_prerequisites

        if command -v oc &> /dev/null && oc whoami &> /dev/null; then
            record_cluster_state
        fi

        if $RUN_GAUGE; then
            run_gauge_suite
            if $RUN_GINKGO; then
                reset_cluster_state
            fi
        fi

        if $RUN_GINKGO; then
            run_ginkgo_suite
        fi
    fi

    compare_results
}

main "$@"
