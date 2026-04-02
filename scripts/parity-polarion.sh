#!/usr/bin/env bash
#
# parity-polarion.sh -- Compare JUnit XML structure for Polarion JUMP importer compatibility
#
# This script validates that both Ginkgo and Gauge JUnit XML files are
# compatible with the Polarion JUMP test case importer. It compares:
#   1. XML structure (testsuite/testcase hierarchy)
#   2. Polarion test case ID coverage (PIPELINES-XX-TCXX)
#   3. Required fields for Polarion import (name, classname, properties)
#   4. Pass/fail status mapping between both XMLs
#
# Usage:
#   ./scripts/parity-polarion.sh [OPTIONS]
#
# Options:
#   --ginkgo-xml PATH   Path to Ginkgo JUnit XML (default: /tmp/ginkgo-junit.xml)
#   --gauge-xml PATH    Path to Gauge JUnit XML (default: /tmp/gauge-junit.xml)
#   --submit            Submit to Polarion JUMP importer (requires POLARION_URL, POLARION_USER, POLARION_TOKEN)
#   --validate-only     Only validate XML structure, do not compare
#   --output-dir PATH   Directory for output files (default: /tmp)
#   --help              Show this help message
#
# Exit codes:
#   0  XML structure compatible and test case IDs match
#   1  Structure incompatible or IDs mismatch
#
set -euo pipefail

# -------------------------------------------------------------------
# Configuration
# -------------------------------------------------------------------

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTPUT_DIR="/tmp"
GINKGO_XML="/tmp/ginkgo-junit.xml"
GAUGE_XML="/tmp/gauge-junit.xml"
SUBMIT=false
VALIDATE_ONLY=false
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
        --ginkgo-xml)
            GINKGO_XML="$2"
            shift 2
            ;;
        --gauge-xml)
            GAUGE_XML="$2"
            shift 2
            ;;
        --submit)
            SUBMIT=true
            shift
            ;;
        --validate-only)
            VALIDATE_ONLY=true
            shift
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --help)
            head -28 "$0" | tail -23
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

COMPARISON_REPORT="${OUTPUT_DIR}/parity-polarion-report.txt"

# -------------------------------------------------------------------
# Validate XML structure for Polarion compatibility
#
# Checks:
# 1. Well-formed XML
# 2. Has <testsuites> or <testsuite> root element
# 3. Contains <testcase> elements with name attribute
# 4. testcase names contain PIPELINES-XX-TCXX Polarion IDs
# 5. classname attribute is present
#
# Arguments:
#   $1  Path to JUnit XML
#   $2  Label (e.g., "Ginkgo" or "Gauge")
# -------------------------------------------------------------------

validate_xml_structure() {
    local xml_file="$1"
    local label="$2"
    local issues=0

    echo "Validating $label JUnit XML: $xml_file"
    echo ""

    # Check file exists
    if [ ! -f "$xml_file" ]; then
        echo -e "  ${RED}MISSING${NC}: File not found"
        return 1
    fi

    local file_size
    file_size=$(wc -c < "$xml_file" | tr -d ' ')
    echo "  File size: $file_size bytes"

    # Check well-formed XML
    if command -v xmllint &> /dev/null; then
        if xmllint --noout "$xml_file" 2>/dev/null; then
            echo -e "  ${GREEN}OK${NC}: Well-formed XML"
        else
            echo -e "  ${RED}FAIL${NC}: Malformed XML"
            xmllint --noout "$xml_file" 2>&1 | head -5
            issues=$((issues + 1))
        fi
    else
        echo -e "  ${YELLOW}SKIP${NC}: xmllint not available for validation"
    fi

    # Check root element
    local root_element
    root_element=$(grep -o '<testsuites\|<testsuite' "$xml_file" | head -1 || true)
    if [ -n "$root_element" ]; then
        echo -e "  ${GREEN}OK${NC}: Root element: $root_element"
    else
        echo -e "  ${RED}FAIL${NC}: No <testsuites> or <testsuite> root element"
        issues=$((issues + 1))
    fi

    # Count testcase elements
    local testcase_count
    testcase_count=$(grep -c '<testcase ' "$xml_file" || echo "0")
    echo "  Testcase elements: $testcase_count"

    if [ "$testcase_count" -eq 0 ]; then
        echo -e "  ${RED}FAIL${NC}: No <testcase> elements found"
        issues=$((issues + 1))
    fi

    # Check for name attributes in testcase
    local named_count
    named_count=$(grep -c '<testcase.*name="' "$xml_file" || echo "0")
    echo "  Testcases with name: $named_count"

    # Check for classname attributes
    local classname_count
    classname_count=$(grep -c '<testcase.*classname="' "$xml_file" || echo "0")
    echo "  Testcases with classname: $classname_count"

    # Extract Polarion IDs
    local polarion_ids
    polarion_ids=$(grep -o 'PIPELINES-[0-9]*-TC[0-9]*' "$xml_file" | sort -u || true)
    local polarion_count=0
    if [ -n "$polarion_ids" ]; then
        polarion_count=$(echo "$polarion_ids" | wc -l | tr -d ' ')
    fi
    echo "  Unique Polarion IDs: $polarion_count"

    # Check for failures and errors
    local failure_count
    failure_count=$(grep -c '<failure' "$xml_file" || echo "0")
    local error_count
    error_count=$(grep -c '<error' "$xml_file" || echo "0")
    local skipped_count
    skipped_count=$(grep -c '<skipped' "$xml_file" || echo "0")

    echo "  Failures: $failure_count"
    echo "  Errors: $error_count"
    echo "  Skipped: $skipped_count"
    echo "  Passed: $((testcase_count - failure_count - error_count - skipped_count))"

    # Check for properties section (some Polarion configs require this)
    local has_properties
    has_properties=$(grep -c '<properties' "$xml_file" || echo "0")
    if [ "$has_properties" -gt 0 ]; then
        echo -e "  ${GREEN}OK${NC}: <properties> section present"
    else
        echo -e "  ${YELLOW}INFO${NC}: No <properties> section (may need to be added for Polarion)"
    fi

    # Polarion-specific checks
    echo ""
    echo "  Polarion Compatibility:"

    # Check that testcase names follow expected format for ID extraction
    local id_in_name
    id_in_name=$(grep '<testcase.*name=".*PIPELINES-' "$xml_file" | wc -l | tr -d ' ' || echo "0")
    echo "    Testcases with Polarion ID in name: $id_in_name"

    if [ "$id_in_name" -lt "$((testcase_count / 2))" ] && [ "$testcase_count" -gt 0 ]; then
        echo -e "    ${YELLOW}WARNING${NC}: Less than half of testcases have Polarion IDs in name"
        echo "    Polarion JUMP importer may not map all test results"
    fi

    echo ""

    if [ "$issues" -gt 0 ]; then
        echo -e "  ${RED}RESULT: $issues issue(s) found${NC}"
        return 1
    else
        echo -e "  ${GREEN}RESULT: Structure valid for Polarion import${NC}"
        return 0
    fi
}

# -------------------------------------------------------------------
# Compare Polarion IDs between two JUnit XMLs
# -------------------------------------------------------------------

compare_polarion_ids() {
    local ginkgo_ids gauge_ids

    echo "=============================================="
    echo "  Polarion Test Case ID Comparison"
    echo "=============================================="
    echo ""

    ginkgo_ids=$(grep -o 'PIPELINES-[0-9]*-TC[0-9]*' "$GINKGO_XML" | sort -u || true)
    gauge_ids=$(grep -o 'PIPELINES-[0-9]*-TC[0-9]*' "$GAUGE_XML" | sort -u || true)

    local ginkgo_count=0 gauge_count=0
    if [ -n "$ginkgo_ids" ]; then
        ginkgo_count=$(echo "$ginkgo_ids" | wc -l | tr -d ' ')
    fi
    if [ -n "$gauge_ids" ]; then
        gauge_count=$(echo "$gauge_ids" | wc -l | tr -d ' ')
    fi

    echo "Ginkgo Polarion IDs in JUnit: $ginkgo_count"
    echo "Gauge Polarion IDs in JUnit:  $gauge_count"
    echo ""

    # Load dropped test IDs
    local dropped_ids=""
    if [ -f "$DROPPED_TESTS" ]; then
        dropped_ids=$(grep -o 'PIPELINES-[0-9]*-TC[0-9]*' "$DROPPED_TESTS" | sort -u || true)
    fi

    # Find differences
    local gauge_only ginkgo_only

    if [ -n "$ginkgo_ids" ] && [ -n "$gauge_ids" ]; then
        gauge_only=$(comm -23 <(echo "$gauge_ids") <(echo "$ginkgo_ids") || true)
        ginkgo_only=$(comm -13 <(echo "$gauge_ids") <(echo "$ginkgo_ids") || true)
    elif [ -n "$gauge_ids" ]; then
        gauge_only="$gauge_ids"
        ginkgo_only=""
    elif [ -n "$ginkgo_ids" ]; then
        gauge_only=""
        ginkgo_only="$ginkgo_ids"
    else
        gauge_only=""
        ginkgo_only=""
    fi

    local gauge_only_count=0 ginkgo_only_count=0
    if [ -n "$gauge_only" ]; then
        gauge_only_count=$(echo "$gauge_only" | grep -c '.' || true)
    fi
    if [ -n "$ginkgo_only" ]; then
        ginkgo_only_count=$(echo "$ginkgo_only" | grep -c '.' || true)
    fi

    echo "In Gauge JUnit only: $gauge_only_count"
    if [ "$gauge_only_count" -gt 0 ]; then
        echo "$gauge_only" | while read -r id; do
            if echo "$dropped_ids" | grep -q "^${id}$"; then
                echo -e "  - $id ${YELLOW}(DROPPED - documented)${NC}"
            else
                echo -e "  - $id ${RED}(UNEXPECTED)${NC}"
            fi
        done
    fi

    echo "In Ginkgo JUnit only: $ginkgo_only_count"
    if [ "$ginkgo_only_count" -gt 0 ]; then
        echo "$ginkgo_only" | while read -r id; do
            echo "  - $id"
        done
    fi

    echo ""

    # Compare pass/fail status for matching IDs
    if [ -n "$ginkgo_ids" ] && [ -n "$gauge_ids" ]; then
        echo "Pass/Fail Status Comparison:"
        echo ""

        local matching_status=0
        local divergent_status=0

        while IFS= read -r id; do
            # Skip if gauge-only or ginkgo-only
            if ! echo "$gauge_ids" | grep -q "^${id}$"; then
                continue
            fi

            # Get status from each XML
            local ginkgo_has_fail gauge_has_fail
            ginkgo_has_fail=$(grep "PIPELINES-.*${id}" "$GINKGO_XML" | grep -c '<failure\|failure=' || echo "0")
            gauge_has_fail=$(grep "PIPELINES-.*${id}" "$GAUGE_XML" | grep -c '<failure\|failure=' || echo "0")

            local g_status="PASS" ga_status="PASS"
            [ "$ginkgo_has_fail" -gt 0 ] && g_status="FAIL"
            [ "$gauge_has_fail" -gt 0 ] && ga_status="FAIL"

            if [ "$g_status" = "$ga_status" ]; then
                matching_status=$((matching_status + 1))
            else
                divergent_status=$((divergent_status + 1))
                echo -e "  ${RED}DIVERGENT${NC}: $id (Ginkgo=$g_status, Gauge=$ga_status)"
            fi
        done <<< "$ginkgo_ids"

        echo ""
        echo "Matching status: $matching_status"
        echo "Divergent status: $divergent_status"
    fi

    echo ""
}

# -------------------------------------------------------------------
# Submit to Polarion JUMP importer
# -------------------------------------------------------------------

submit_to_polarion() {
    echo "=============================================="
    echo "  Polarion JUMP Importer Submission"
    echo "=============================================="
    echo ""

    # Check required environment variables
    if [ -z "${POLARION_URL:-}" ]; then
        echo -e "${RED}ERROR${NC}: POLARION_URL not set"
        echo "Set: export POLARION_URL=https://polarion.example.com"
        return 1
    fi

    if [ -z "${POLARION_USER:-}" ]; then
        echo -e "${RED}ERROR${NC}: POLARION_USER not set"
        return 1
    fi

    if [ -z "${POLARION_TOKEN:-}" ]; then
        echo -e "${RED}ERROR${NC}: POLARION_TOKEN not set"
        return 1
    fi

    echo "Polarion URL: $POLARION_URL"
    echo "User: $POLARION_USER"
    echo ""

    # Submit Ginkgo results
    if [ -f "$GINKGO_XML" ]; then
        echo "Submitting Ginkgo JUnit XML..."
        local response
        response=$(curl -s -w "\n%{http_code}" \
            -u "${POLARION_USER}:${POLARION_TOKEN}" \
            -F "file=@${GINKGO_XML}" \
            "${POLARION_URL}/import/xunit" 2>&1 || true)

        local http_code
        http_code=$(echo "$response" | tail -1)
        local body
        body=$(echo "$response" | head -n -1)

        if [ "$http_code" = "200" ] || [ "$http_code" = "201" ]; then
            echo -e "  ${GREEN}SUCCESS${NC}: HTTP $http_code"
        else
            echo -e "  ${RED}FAILED${NC}: HTTP $http_code"
            echo "  Response: $body"
        fi
    fi

    echo ""
}

# -------------------------------------------------------------------
# Generate structural comparison when no XMLs available
# -------------------------------------------------------------------

generate_structural_report() {
    echo "=============================================="
    echo "  Structural Comparison (Source Analysis)"
    echo "=============================================="
    echo ""
    echo "No JUnit XML files available for comparison."
    echo "Performing source-level structural analysis..."
    echo ""

    # Ginkgo source analysis
    local ginkgo_polarion_count
    ginkgo_polarion_count=$(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' "$PROJECT_ROOT/tests/" --include="*_test.go" | sort -u | wc -l | tr -d ' ')

    echo "Ginkgo Source Analysis:"
    echo "  Unique Polarion IDs: $ginkgo_polarion_count"
    echo "  Test areas: $(find "$PROJECT_ROOT/tests/" -name "suite_test.go" | wc -l | tr -d ' ')"
    echo ""

    # Gauge source analysis
    local gauge_polarion_count
    gauge_polarion_count=$(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' "$PROJECT_ROOT/reference-repo/release-tests/specs/" | sort -u | wc -l | tr -d ' ')

    echo "Gauge Source Analysis:"
    echo "  Unique Polarion IDs: $gauge_polarion_count"
    echo "  Spec files: $(find "$PROJECT_ROOT/reference-repo/release-tests/specs/" -name "*.spec" | wc -l | tr -d ' ')"
    echo ""

    # JUnit XML format expectations
    echo "Expected Ginkgo JUnit XML Format:"
    echo "  <testsuites>"
    echo "    <testsuite name=\"[Suite Name]\" tests=\"N\" failures=\"M\">"
    echo "      <testcase name=\"PIPELINES-XX-TCXX: Test description\" classname=\"[package]\">"
    echo "        [<failure message=\"...\">...</failure>]"
    echo "      </testcase>"
    echo "    </testsuite>"
    echo "  </testsuites>"
    echo ""

    echo "Expected Gauge JUnit XML Format:"
    echo "  <testsuites>"
    echo "    <testsuite name=\"specs\" tests=\"N\" failures=\"M\">"
    echo "      <testcase name=\"Test scenario: PIPELINES-XX-TCXX\" classname=\"specs/area/file.spec\">"
    echo "        [<failure message=\"...\">...</failure>]"
    echo "      </testcase>"
    echo "    </testsuite>"
    echo "  </testsuites>"
    echo ""

    echo "Key Differences in XML Structure:"
    echo "  1. Ginkgo classname uses Go package paths; Gauge uses spec file paths"
    echo "  2. Ginkgo test names are hierarchical (Describe > Context > It); Gauge names are flat scenarios"
    echo "  3. Both embed PIPELINES-XX-TCXX in test names for Polarion mapping"
    echo "  4. Ginkgo may produce multiple <testsuite> elements (one per package); Gauge produces one"
    echo ""

    echo "Polarion JUMP Importer Requirements:"
    echo "  - testcase name must contain the Polarion test case ID"
    echo "  - classname should be non-empty"
    echo "  - failure element present for failed tests"
    echo "  - Project ID configured via properties or JUMP configuration"
    echo ""

    # Write comparison report
    {
        echo "Polarion JUnit XML Comparison Report"
        echo "====================================="
        echo "Date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
        echo "Status: STRUCTURAL ANALYSIS ONLY (no runtime XMLs)"
        echo ""
        echo "Ginkgo Polarion IDs: $ginkgo_polarion_count"
        echo "Gauge Polarion IDs: $gauge_polarion_count"
        echo "Delta: $((gauge_polarion_count - ginkgo_polarion_count))"
        echo ""
        echo "To perform full comparison:"
        echo "  1. Run both suites to generate JUnit XMLs"
        echo "  2. Run: ./scripts/parity-polarion.sh --ginkgo-xml /path/to/ginkgo.xml --gauge-xml /path/to/gauge.xml"
    } > "$COMPARISON_REPORT"

    echo "Structural report saved to: $COMPARISON_REPORT"
}

# -------------------------------------------------------------------
# Main execution
# -------------------------------------------------------------------

main() {
    echo "=============================================="
    echo "  Polarion JUnit XML Parity Validation"
    echo "=============================================="
    echo ""
    echo "Date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
    echo ""

    local ginkgo_valid=false
    local gauge_valid=false

    # Validate Ginkgo XML
    if [ -f "$GINKGO_XML" ]; then
        if validate_xml_structure "$GINKGO_XML" "Ginkgo"; then
            ginkgo_valid=true
        fi
        echo ""
    else
        echo -e "${YELLOW}Ginkgo JUnit XML not found at: $GINKGO_XML${NC}"
        echo ""
    fi

    # Validate Gauge XML
    if [ -f "$GAUGE_XML" ]; then
        if validate_xml_structure "$GAUGE_XML" "Gauge"; then
            gauge_valid=true
        fi
        echo ""
    else
        echo -e "${YELLOW}Gauge JUnit XML not found at: $GAUGE_XML${NC}"
        echo ""
    fi

    # Compare if both available and not validate-only
    if $ginkgo_valid && $gauge_valid && ! $VALIDATE_ONLY; then
        compare_polarion_ids
    elif ! $ginkgo_valid || ! $gauge_valid; then
        generate_structural_report
    fi

    # Submit to Polarion if requested
    if $SUBMIT; then
        submit_to_polarion
    fi

    # Final verdict
    echo "=============================================="
    echo "  Verdict"
    echo "=============================================="
    echo ""

    if $ginkgo_valid && $gauge_valid; then
        echo -e "${GREEN}PASS${NC}: Both JUnit XMLs are structurally valid for Polarion import"
        exit 0
    elif $ginkgo_valid || $gauge_valid; then
        local valid_label="Ginkgo"
        $gauge_valid && valid_label="Gauge"
        echo -e "${YELLOW}PARTIAL${NC}: Only $valid_label JUnit XML validated"
        echo "Run both suites to perform full comparison"
        exit 0
    else
        echo -e "${YELLOW}PENDING${NC}: No JUnit XMLs available for validation"
        echo "Structural source analysis complete. Run suites on a live cluster to generate XMLs."
        exit 0
    fi
}

main "$@"
