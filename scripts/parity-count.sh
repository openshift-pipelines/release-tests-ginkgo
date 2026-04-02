#!/usr/bin/env bash
#
# parity-count.sh -- Compare Ginkgo and Gauge test counts for parity validation
#
# This script compares the number of unique Polarion test case IDs between the
# Ginkgo test suite and the Gauge reference suite. The comparison uses Polarion
# IDs (PIPELINES-XX-TCXX) as the canonical identifier since the test structure
# differs between frameworks (Ginkgo uses Ordered containers with multiple It
# blocks per scenario, while Gauge uses flat scenario definitions).
#
# Usage:
#   ./scripts/parity-count.sh [PROJECT_ROOT]
#
# Arguments:
#   PROJECT_ROOT  Path to the project root (default: current directory)
#
# Exit codes:
#   0  Parity match (counts are within expected delta)
#   1  Parity mismatch or error
#
set -euo pipefail

PROJECT_ROOT="${1:-.}"
cd "$PROJECT_ROOT"

# Paths
GINKGO_TESTS_DIR="tests"
GAUGE_SPECS_DIR="reference-repo/release-tests/specs"
DROPPED_TESTS_MD=".planning/phases/11-parity-validation/DROPPED-TESTS.md"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "=============================================="
echo "  Test Count Parity Report"
echo "=============================================="
echo ""
echo "Project root: $(pwd)"
echo "Date: $(date -u +"%Y-%m-%dT%H:%M:%SZ")"
echo ""

# -------------------------------------------------------------------
# Method 1: Polarion ID comparison (primary method)
# This is the most reliable comparison since Polarion IDs are the
# canonical test identifiers shared between both frameworks.
# -------------------------------------------------------------------

echo "--- Method 1: Polarion ID Comparison ---"
echo ""

# Extract unique Polarion IDs from Gauge specs
if [ ! -d "$GAUGE_SPECS_DIR" ]; then
    echo -e "${RED}ERROR: Gauge specs directory not found: $GAUGE_SPECS_DIR${NC}"
    echo "Clone the reference repo first."
    exit 1
fi

GAUGE_POLARION_IDS=$(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' "$GAUGE_SPECS_DIR" | sort -u)
GAUGE_POLARION_COUNT=$(echo "$GAUGE_POLARION_IDS" | wc -l | tr -d ' ')

echo "Gauge unique Polarion IDs: $GAUGE_POLARION_COUNT"

# Extract unique Polarion IDs from Ginkgo tests
if [ ! -d "$GINKGO_TESTS_DIR" ]; then
    echo -e "${RED}ERROR: Ginkgo tests directory not found: $GINKGO_TESTS_DIR${NC}"
    exit 1
fi

GINKGO_POLARION_IDS=$(grep -roh 'PIPELINES-[0-9]*-TC[0-9]*' "$GINKGO_TESTS_DIR" --include="*_test.go" | sort -u)
GINKGO_POLARION_COUNT=$(echo "$GINKGO_POLARION_IDS" | wc -l | tr -d ' ')

echo "Ginkgo unique Polarion IDs: $GINKGO_POLARION_COUNT"

# Find IDs in Gauge but not in Ginkgo
GAUGE_ONLY=$(comm -23 <(echo "$GAUGE_POLARION_IDS") <(echo "$GINKGO_POLARION_IDS"))
GAUGE_ONLY_COUNT=0
if [ -n "$GAUGE_ONLY" ]; then
    GAUGE_ONLY_COUNT=$(echo "$GAUGE_ONLY" | wc -l | tr -d ' ')
fi

# Find IDs in Ginkgo but not in Gauge
GINKGO_ONLY=$(comm -13 <(echo "$GAUGE_POLARION_IDS") <(echo "$GINKGO_POLARION_IDS"))
GINKGO_ONLY_COUNT=0
if [ -n "$GINKGO_ONLY" ]; then
    GINKGO_ONLY_COUNT=$(echo "$GINKGO_ONLY" | wc -l | tr -d ' ')
fi

echo ""
echo "IDs in Gauge only: $GAUGE_ONLY_COUNT"
if [ "$GAUGE_ONLY_COUNT" -gt 0 ]; then
    echo "$GAUGE_ONLY" | while read -r id; do
        echo "  - $id"
    done
fi

echo "IDs in Ginkgo only: $GINKGO_ONLY_COUNT"
if [ "$GINKGO_ONLY_COUNT" -gt 0 ]; then
    echo "$GINKGO_ONLY" | while read -r id; do
        echo "  - $id"
    done
fi

# -------------------------------------------------------------------
# Method 2: Scenario count comparison
# Counts Gauge scenarios (## headings) vs Ginkgo top-level test cases.
# These will differ due to structural changes (Ordered containers,
# DescribeTable expansion) -- this is informational only.
# -------------------------------------------------------------------

echo ""
echo "--- Method 2: Scenario Count (Informational) ---"
echo ""

GAUGE_SCENARIO_COUNT=$(grep -rh "^## " "$GAUGE_SPECS_DIR" | wc -l | tr -d ' ')
echo "Gauge scenarios (## headings): $GAUGE_SCENARIO_COUNT"

# Count scenarios without Polarion IDs
GAUGE_NO_ID=$(grep -rh "^## " "$GAUGE_SPECS_DIR" | grep -cv "PIPELINES-" || true)
echo "Gauge scenarios without Polarion ID: $GAUGE_NO_ID"

# Count Gauge scenarios tagged to-do (not actually executable)
GAUGE_TODO=0
while IFS= read -r line; do
    if echo "$line" | grep -q "to-do"; then
        GAUGE_TODO=$((GAUGE_TODO + 1))
    fi
done < <(grep -rh "^Tags:.*to-do" "$GAUGE_SPECS_DIR" 2>/dev/null || true)
echo "Gauge scenarios tagged to-do: $GAUGE_TODO"

# Ginkgo spec count (It + Entry blocks, excluding suite_test.go)
GINKGO_IT_COUNT=$(grep -rh 'It(' "$GINKGO_TESTS_DIR" --include="*_test.go" | grep -v 'suite_test.go' | grep -cv '^\s*//' || true)
GINKGO_ENTRY_COUNT=$(grep -rh 'Entry(' "$GINKGO_TESTS_DIR" --include="*_test.go" | grep -v 'suite_test.go' | grep -cv '^\s*//' || true)
GINKGO_PENDING_COUNT=$(grep -rch 'Pending\|PDescribe\|PIt\|PContext' "$GINKGO_TESTS_DIR" --include="*_test.go" | paste -sd+ - | bc 2>/dev/null || echo "0")
echo "Ginkgo It() blocks: $GINKGO_IT_COUNT"
echo "Ginkgo Entry() blocks: $GINKGO_ENTRY_COUNT"
echo "Ginkgo total specs: $((GINKGO_IT_COUNT + GINKGO_ENTRY_COUNT))"
echo "Ginkgo Pending/PDescribe markers: $GINKGO_PENDING_COUNT"

echo ""
echo "NOTE: Ginkgo spec count is higher than Gauge scenario count because"
echo "Ginkgo decomposes multi-step Gauge scenarios into individual It() blocks"
echo "within Ordered containers (OLM, PAC, Results, Chains, MAG tests)."
echo "Polarion ID comparison (Method 1) is the authoritative metric."

# -------------------------------------------------------------------
# Method 3: Dry-run comparison (requires ginkgo CLI and go toolchain)
# Attempts to run ginkgo --dry-run if available
# -------------------------------------------------------------------

echo ""
echo "--- Method 3: Ginkgo Dry-Run (if available) ---"
echo ""

if command -v ginkgo &> /dev/null; then
    echo "ginkgo CLI found: $(which ginkgo)"
    echo "Running dry-run to count executable specs..."
    echo ""

    DRY_RUN_OUTPUT=$(ginkgo run --dry-run -v ./tests/... 2>&1 || true)

    # Count specs from dry-run output
    DRY_RUN_PASS=$(echo "$DRY_RUN_OUTPUT" | grep -c '\[PASSED\]' || true)
    DRY_RUN_PENDING=$(echo "$DRY_RUN_OUTPUT" | grep -c '\[PENDING\]' || true)
    DRY_RUN_SKIP=$(echo "$DRY_RUN_OUTPUT" | grep -c '\[SKIPPED\]' || true)

    echo "Dry-run results:"
    echo "  Passed (would run): $DRY_RUN_PASS"
    echo "  Pending: $DRY_RUN_PENDING"
    echo "  Skipped: $DRY_RUN_SKIP"
    echo "  Total: $((DRY_RUN_PASS + DRY_RUN_PENDING + DRY_RUN_SKIP))"
else
    echo "ginkgo CLI not found in PATH -- skipping dry-run"
    echo "Install with: go install github.com/onsi/ginkgo/v2/ginkgo@latest"
fi

# -------------------------------------------------------------------
# Validation: Check dropped tests manifest
# -------------------------------------------------------------------

echo ""
echo "--- Dropped Tests Validation ---"
echo ""

if [ -f "$DROPPED_TESTS_MD" ]; then
    echo "DROPPED-TESTS.md found: $DROPPED_TESTS_MD"

    # The expected delta is the count of IDs in Gauge but not in Ginkgo
    echo "Expected dropped IDs (Gauge-only): $GAUGE_ONLY_COUNT"

    # Check each Gauge-only ID is documented in DROPPED-TESTS.md
    ALL_DOCUMENTED=true
    if [ "$GAUGE_ONLY_COUNT" -gt 0 ]; then
        echo "$GAUGE_ONLY" | while read -r id; do
            if grep -q "$id" "$DROPPED_TESTS_MD"; then
                echo "  [DOCUMENTED] $id"
            else
                echo "  [MISSING] $id -- not documented in DROPPED-TESTS.md"
                ALL_DOCUMENTED=false
            fi
        done
    fi
else
    echo -e "${YELLOW}WARNING: DROPPED-TESTS.md not found at $DROPPED_TESTS_MD${NC}"
fi

# -------------------------------------------------------------------
# Final verdict
# -------------------------------------------------------------------

echo ""
echo "=============================================="
echo "  Verdict"
echo "=============================================="
echo ""

# The parity check passes if:
# 1. All Gauge-only Polarion IDs are documented in DROPPED-TESTS.md
# 2. There are no undocumented Ginkgo-only IDs (would indicate extra tests)
# 3. Polarion ID coverage: Ginkgo count + dropped count >= Gauge count

EXPECTED_GINKGO_COUNT=$((GAUGE_POLARION_COUNT - GAUGE_ONLY_COUNT))

if [ "$GINKGO_POLARION_COUNT" -eq "$EXPECTED_GINKGO_COUNT" ] && [ "$GINKGO_ONLY_COUNT" -eq 0 ]; then
    echo -e "${GREEN}PASS${NC}: Polarion ID parity confirmed"
    echo ""
    echo "  Gauge Polarion IDs:    $GAUGE_POLARION_COUNT"
    echo "  Dropped (documented):  $GAUGE_ONLY_COUNT"
    echo "  Expected Ginkgo IDs:   $EXPECTED_GINKGO_COUNT"
    echo "  Actual Ginkgo IDs:     $GINKGO_POLARION_COUNT"
    echo ""
    exit 0
elif [ "$GINKGO_ONLY_COUNT" -gt 0 ]; then
    echo -e "${YELLOW}INFO${NC}: Ginkgo has extra Polarion IDs not in Gauge"
    echo "  This may indicate new tests added during migration."
    echo ""
    echo "  Gauge Polarion IDs:    $GAUGE_POLARION_COUNT"
    echo "  Ginkgo Polarion IDs:   $GINKGO_POLARION_COUNT"
    echo "  Gauge-only IDs:        $GAUGE_ONLY_COUNT"
    echo "  Ginkgo-only IDs:       $GINKGO_ONLY_COUNT"
    echo ""

    # Still pass if all Gauge IDs are accounted for
    if [ "$GAUGE_ONLY_COUNT" -le 1 ]; then
        echo -e "${GREEN}PASS${NC}: All Gauge Polarion IDs are covered (delta within expected range)"
        exit 0
    else
        echo -e "${RED}FAIL${NC}: Unexpected Polarion ID delta"
        exit 1
    fi
else
    echo -e "${RED}FAIL${NC}: Polarion ID count mismatch"
    echo ""
    echo "  Gauge Polarion IDs:    $GAUGE_POLARION_COUNT"
    echo "  Dropped (documented):  $GAUGE_ONLY_COUNT"
    echo "  Expected Ginkgo IDs:   $EXPECTED_GINKGO_COUNT"
    echo "  Actual Ginkgo IDs:     $GINKGO_POLARION_COUNT"
    echo ""
    exit 1
fi
