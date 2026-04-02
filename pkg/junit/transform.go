// Package junit provides JUnit XML post-processing utilities for Polarion compatibility.
//
// Ginkgo produces standard JUnit XML via --junit-report, but the Polarion JUMP importer
// requires additional properties (project ID, test case IDs) that are not present in
// standard JUnit output. This package bridges that gap.
package junit

import (
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
)

// polarionIDPattern matches Polarion test case IDs like PIPELINES-22-TC01
var polarionIDPattern = regexp.MustCompile(`PIPELINES-\d+-TC\d+`)

// --- JUnit XML Schema structs ---

// TestSuites is the root element of a JUnit XML report.
type TestSuites struct {
	XMLName    xml.Name    `xml:"testsuites"`
	TestSuites []TestSuite `xml:"testsuite"`
}

// TestSuite represents a single test suite in JUnit XML.
type TestSuite struct {
	XMLName    xml.Name   `xml:"testsuite"`
	Name       string     `xml:"name,attr"`
	Tests      int        `xml:"tests,attr"`
	Failures   int        `xml:"failures,attr,omitempty"`
	Errors     int        `xml:"errors,attr,omitempty"`
	Skipped    int        `xml:"skipped,attr,omitempty"`
	Time       string     `xml:"time,attr,omitempty"`
	Timestamp  string     `xml:"timestamp,attr,omitempty"`
	Properties *Properties `xml:"properties,omitempty"`
	TestCases  []TestCase `xml:"testcase"`
	SystemOut  string     `xml:"system-out,omitempty"`
	SystemErr  string     `xml:"system-err,omitempty"`
}

// TestCase represents a single test case in JUnit XML.
type TestCase struct {
	XMLName    xml.Name   `xml:"testcase"`
	Name       string     `xml:"name,attr"`
	Classname  string     `xml:"classname,attr,omitempty"`
	Time       string     `xml:"time,attr,omitempty"`
	Status     string     `xml:"status,attr,omitempty"`
	Properties *Properties `xml:"properties,omitempty"`
	Failure    *Failure   `xml:"failure,omitempty"`
	Error      *Error     `xml:"error,omitempty"`
	Skipped    *Skipped   `xml:"skipped,omitempty"`
	SystemOut  string     `xml:"system-out,omitempty"`
	SystemErr  string     `xml:"system-err,omitempty"`
}

// Properties is a collection of property elements.
type Properties struct {
	Properties []Property `xml:"property"`
}

// Property is a key-value pair in JUnit XML.
type Property struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Failure represents a test failure.
type Failure struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// Error represents a test error.
type Error struct {
	Message string `xml:"message,attr,omitempty"`
	Type    string `xml:"type,attr,omitempty"`
	Body    string `xml:",chardata"`
}

// Skipped represents a skipped test.
type Skipped struct {
	Message string `xml:"message,attr,omitempty"`
}

// TransformForPolarion reads a JUnit XML file, injects Polarion-required properties,
// and writes the transformed XML to the output path.
//
// It performs the following transformations:
//  1. Injects suite-level <properties> with polarion-project-id and polarion-testrun-title
//  2. Extracts PIPELINES-XX-TCNN IDs from test case names and adds them as
//     polarion-testcase-id properties on each test case
//
// If projectID is empty, it defaults to "PIPELINES".
func TransformForPolarion(inputPath, outputPath, projectID string) error {
	if projectID == "" {
		projectID = "PIPELINES"
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read input file %s: %w", inputPath, err)
	}

	// Try parsing as <testsuites> (multiple suites)
	var suites TestSuites
	if err := xml.Unmarshal(data, &suites); err != nil {
		// Try parsing as a single <testsuite>
		var single TestSuite
		if err2 := xml.Unmarshal(data, &single); err2 != nil {
			return fmt.Errorf("failed to parse JUnit XML: %w (also tried single suite: %v)", err, err2)
		}
		suites.TestSuites = []TestSuite{single}
	}

	// If no test suites were found in <testsuites> wrapper, it might be empty
	if len(suites.TestSuites) == 0 {
		return fmt.Errorf("no test suites found in %s", inputPath)
	}

	// Transform each suite
	for i := range suites.TestSuites {
		suite := &suites.TestSuites[i]

		// 1. Inject suite-level properties
		suiteProps := []Property{
			{Name: "polarion-project-id", Value: projectID},
			{Name: "polarion-testrun-title", Value: fmt.Sprintf("OpenShift Pipelines Ginkgo - %s", suite.Name)},
		}
		if suite.Properties == nil {
			suite.Properties = &Properties{}
		}
		// Prepend Polarion properties (don't overwrite existing ones)
		suite.Properties.Properties = append(suiteProps, suite.Properties.Properties...)

		// 2. Extract Polarion test case IDs and add to each test case
		for j := range suite.TestCases {
			tc := &suite.TestCases[j]
			id := polarionIDPattern.FindString(tc.Name)
			if id != "" {
				if tc.Properties == nil {
					tc.Properties = &Properties{}
				}
				tc.Properties.Properties = append(tc.Properties.Properties, Property{
					Name:  "polarion-testcase-id",
					Value: id,
				})
			}
		}
	}

	// Marshal back to XML
	output, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal transformed XML: %w", err)
	}

	// Add XML declaration
	xmlOutput := []byte(xml.Header)
	xmlOutput = append(xmlOutput, output...)

	if err := os.WriteFile(outputPath, xmlOutput, 0644); err != nil {
		return fmt.Errorf("failed to write output file %s: %w", outputPath, err)
	}

	return nil
}

// ExtractPolarionIDs returns all Polarion test case IDs found in the given JUnit XML file.
func ExtractPolarionIDs(inputPath string) ([]string, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", inputPath, err)
	}

	var suites TestSuites
	if err := xml.Unmarshal(data, &suites); err != nil {
		var single TestSuite
		if err2 := xml.Unmarshal(data, &single); err2 != nil {
			return nil, fmt.Errorf("failed to parse JUnit XML: %w", err)
		}
		suites.TestSuites = []TestSuite{single}
	}

	var ids []string
	for _, suite := range suites.TestSuites {
		for _, tc := range suite.TestCases {
			id := polarionIDPattern.FindString(tc.Name)
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids, nil
}
