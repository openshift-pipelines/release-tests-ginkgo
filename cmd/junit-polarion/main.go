// junit-polarion transforms Ginkgo JUnit XML output into Polarion-compatible format.
//
// It injects Polarion-specific properties at the test suite level and extracts
// test case IDs (PIPELINES-XX-TCXX) from spec names to map them to Polarion
// test case identifiers.
//
// Usage:
//
//	junit-polarion --input junit-report.xml [--output polarion-report.xml] [--project-id PIPELINES]
package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// JUnit XML structures

// JUnitTestSuites is the top-level container for JUnit XML output.
type JUnitTestSuites struct {
	XMLName    xml.Name         `xml:"testsuites"`
	TestSuites []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a single test suite within the JUnit XML.
type JUnitTestSuite struct {
	Name       string          `xml:"name,attr"`
	Tests      int             `xml:"tests,attr"`
	Failures   int             `xml:"failures,attr"`
	Errors     int             `xml:"errors,attr"`
	Time       string          `xml:"time,attr"`
	Properties []JUnitProperty `xml:"properties>property"`
	TestCases  []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents a single test case within a test suite.
type JUnitTestCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Time      string        `xml:"time,attr"`
	Failure   *JUnitFailure `xml:"failure,omitempty"`
	Skipped   *JUnitSkipped `xml:"skipped,omitempty"`
	SystemOut string        `xml:"system-out,omitempty"`
}

// JUnitFailure represents a test case failure.
type JUnitFailure struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// JUnitSkipped represents a skipped test case.
type JUnitSkipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// JUnitProperty represents a key-value property in the JUnit XML.
type JUnitProperty struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// polarionTestCaseIDPattern matches Polarion test case IDs in spec names.
// Format: PIPELINES-XX-TCXX (e.g., PIPELINES-07-TC01, PIPELINES-32-TC01)
var polarionTestCaseIDPattern = regexp.MustCompile(`(PIPELINES-\d+-TC\d+)`)

func main() {
	var (
		inputPath  string
		outputPath string
		projectID  string
	)

	flag.StringVar(&inputPath, "input", "", "Path to Ginkgo JUnit XML file (required)")
	flag.StringVar(&inputPath, "i", "", "Path to Ginkgo JUnit XML file (shorthand)")
	flag.StringVar(&outputPath, "output", "", "Path for transformed XML output (defaults to overwriting input)")
	flag.StringVar(&outputPath, "o", "", "Path for transformed XML output (shorthand)")
	flag.StringVar(&projectID, "project-id", "PIPELINES", "Polarion project ID")
	flag.StringVar(&projectID, "p", "PIPELINES", "Polarion project ID (shorthand)")
	flag.Parse()

	if inputPath == "" {
		fmt.Fprintln(os.Stderr, "Error: --input is required")
		flag.Usage()
		os.Exit(1)
	}

	if outputPath == "" {
		outputPath = inputPath
	}

	// Read input XML
	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading input file: %v\n", err)
		os.Exit(1)
	}

	// Parse JUnit XML
	var testSuites JUnitTestSuites
	if err := xml.Unmarshal(data, &testSuites); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing JUnit XML: %v\n", err)
		os.Exit(1)
	}

	// Transform for Polarion compatibility
	timestamp := time.Now().Format("20060102-150405")
	totalCases := 0
	mappedCases := 0
	unmappedCases := 0

	for i := range testSuites.TestSuites {
		suite := &testSuites.TestSuites[i]

		// Inject suite-level Polarion properties
		suite.Properties = appendIfMissing(suite.Properties, JUnitProperty{
			Name:  "polarion-project-id",
			Value: projectID,
		})
		suite.Properties = appendIfMissing(suite.Properties, JUnitProperty{
			Name:  "polarion-testrun-id",
			Value: fmt.Sprintf("%s-%s", sanitizeName(suite.Name), timestamp),
		})

		// Process test cases
		for j := range suite.TestCases {
			tc := &suite.TestCases[j]
			totalCases++

			// Extract Polarion test case ID from spec name
			matches := polarionTestCaseIDPattern.FindStringSubmatch(tc.Name)
			if len(matches) > 1 {
				// Add test case ID as a property via the classname or system-out
				// Since JUnit testcase doesn't have per-case properties in standard schema,
				// we embed the Polarion ID in the classname field
				if tc.Classname == "" {
					tc.Classname = matches[1]
				} else if !strings.Contains(tc.Classname, matches[1]) {
					tc.Classname = tc.Classname + " " + matches[1]
				}
				mappedCases++
			} else {
				unmappedCases++
			}

			// Ensure system-out exists (Polarion requires it)
			// An empty string will produce <system-out></system-out> in XML
			// which is handled by omitempty -- we need to ensure non-empty for Polarion
			if tc.SystemOut == "" {
				tc.SystemOut = " "
			}
		}
	}

	// Write transformed XML
	output, err := xml.MarshalIndent(testSuites, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling XML: %v\n", err)
		os.Exit(1)
	}

	xmlHeader := []byte(xml.Header)
	fullOutput := append(xmlHeader, output...)

	if err := os.WriteFile(outputPath, fullOutput, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing output file: %v\n", err)
		os.Exit(1)
	}

	// Print summary to stderr
	fmt.Fprintf(os.Stderr, "Polarion JUnit Transformation Summary:\n")
	fmt.Fprintf(os.Stderr, "  Test suites processed: %d\n", len(testSuites.TestSuites))
	fmt.Fprintf(os.Stderr, "  Total test cases: %d\n", totalCases)
	fmt.Fprintf(os.Stderr, "  Mapped with Polarion IDs: %d\n", mappedCases)
	if unmappedCases > 0 {
		fmt.Fprintf(os.Stderr, "  WARNING: Unmapped test cases: %d (no PIPELINES-XX-TCXX in name)\n", unmappedCases)
	}
	fmt.Fprintf(os.Stderr, "  Output written to: %s\n", outputPath)
}

// appendIfMissing adds a property only if one with the same name doesn't already exist.
func appendIfMissing(props []JUnitProperty, prop JUnitProperty) []JUnitProperty {
	for _, p := range props {
		if p.Name == prop.Name {
			return props
		}
	}
	return append(props, prop)
}

// sanitizeName replaces spaces and special characters with hyphens for use in IDs.
func sanitizeName(name string) string {
	replacer := strings.NewReplacer(" ", "-", "/", "-", "\\", "-", ":", "-")
	return strings.ToLower(replacer.Replace(name))
}
