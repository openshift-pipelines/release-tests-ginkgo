#!/usr/bin/env bash
# validate-junit.sh - Validates JUnit XML output for Polarion compatibility
#
# Usage:
#   ./scripts/validate-junit.sh <junit-xml-file> [expected-count]
#
# Checks:
#   1. File exists and is valid XML
#   2. Every <testcase> has a Polarion test case ID (PIPELINES-XX-TCNN)
#   3. Total test case count matches expected (if provided)
#
# Exit codes:
#   0 - All checks pass
#   1 - One or more checks fail

set -euo pipefail

JUNIT_FILE="${1:-}"
EXPECTED_COUNT="${2:-}"

if [ -z "$JUNIT_FILE" ]; then
    echo "Usage: $0 <junit-xml-file> [expected-count]"
    exit 1
fi

if [ ! -f "$JUNIT_FILE" ]; then
    echo "ERROR: File not found: $JUNIT_FILE"
    exit 1
fi

# Check valid XML
if command -v xmllint &>/dev/null; then
    if ! xmllint --noout "$JUNIT_FILE" 2>/dev/null; then
        echo "ERROR: File is not valid XML: $JUNIT_FILE"
        exit 1
    fi
fi

# Count total test cases
TOTAL=$(grep -c '<testcase ' "$JUNIT_FILE" 2>/dev/null || echo "0")

# Count test cases with Polarion IDs (PIPELINES-XX-TCNN pattern in name attribute)
WITH_ID=$(grep '<testcase ' "$JUNIT_FILE" | grep -c 'PIPELINES-[0-9]*-TC[0-9]*' 2>/dev/null || echo "0")

# Count test cases without Polarion IDs
WITHOUT_ID=$((TOTAL - WITH_ID))

# Determine status
STATUS="PASS"
ERRORS=""

if [ "$WITHOUT_ID" -gt 0 ]; then
    STATUS="FAIL"
    ERRORS="${ERRORS}  - $WITHOUT_ID test case(s) missing Polarion ID\n"
fi

if [ "$TOTAL" -eq 0 ]; then
    STATUS="FAIL"
    ERRORS="${ERRORS}  - No test cases found in XML\n"
fi

if [ -n "$EXPECTED_COUNT" ] && [ "$TOTAL" -ne "$EXPECTED_COUNT" ]; then
    STATUS="FAIL"
    ERRORS="${ERRORS}  - Expected $EXPECTED_COUNT test cases but found $TOTAL\n"
fi

# Print summary
echo ""
echo "JUnit Validation Report"
echo "======================="
echo "File: $JUNIT_FILE"
echo "Total test cases: $TOTAL"
echo "With Polarion ID: $WITH_ID"
echo "Without Polarion ID: $WITHOUT_ID"

if [ -n "$EXPECTED_COUNT" ]; then
    echo "Expected count: $EXPECTED_COUNT"
fi

echo ""
echo "Status: $STATUS"

if [ "$STATUS" = "FAIL" ]; then
    echo ""
    echo "Errors:"
    echo -e "$ERRORS"
    exit 1
fi

exit 0
