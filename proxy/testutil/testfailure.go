package testutil

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

// TestFailure represents a single test failure with detailed information
// about what failed, where it failed, and why it failed.
type TestFailure struct {
	// TestName is the name of the test that failed
	TestName string `json:"test_name"`

	// FilePath is the path to the file containing the test
	FilePath string `json:"file_path"`

	// LineNumber is the line number where the failure occurred
	LineNumber int `json:"line_number"`

	// ErrorMessage describes what went wrong
	ErrorMessage string `json:"error_message"`

	// StackTrace contains the full stack trace if available
	StackTrace string `json:"stack_trace,omitempty"`
}

// ParseTestFailures parses Go test output and extracts failure information.
// It handles the standard Go test output format including:
// - FAIL lines marking failed tests
// - File:line error messages
// - Stack traces
//
// Parameters:
//   - output: The raw test output bytes
//
// Returns:
//   - []TestFailure: List of parsed test failures (empty if none found)
//   - error: An error if parsing fails (not for "no failures" case)
//
// Example usage:
//   data, _ := testutil.ReadTestOutput("testdata/failures.txt")
//   failures, err := testutil.ParseTestFailures(data)
//   if err != nil {
//       log.Fatalf("Failed to parse test failures: %v", err)
//   }
//   for _, failure := range failures {
//       fmt.Printf("%s failed at %s:%d: %s\n", failure.TestName, failure.FilePath, failure.LineNumber, failure.ErrorMessage)
//   }
func ParseTestFailures(output []byte) ([]TestFailure, error) {
	var failures []TestFailure
	var currentFailure *TestFailure
	var pendingFile, pendingLine, pendingMessage string
	var pendingStackTrace strings.Builder
	var inStackTrace bool

	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Regex patterns
	// Matches: --- FAIL: TestName (0.05s)
	failLinePattern := regexp.MustCompile(`^--- FAIL:\s+(\S+)\s+\(`)
	// Matches:    file_test.go:45: error message
	fileLinePattern := regexp.MustCompile(`^\s+(\S+\.go):(\d+):\s*(.*)`)
	// Matches: goroutine N [running]:
	stackTracePattern := regexp.MustCompile(`^goroutine \d+ \[`)

	for scanner.Scan() {
		line := scanner.Text()

		// Check for file:line pattern (comes before FAIL line in Go output)
		if matches := fileLinePattern.FindStringSubmatch(line); matches != nil {
			pendingFile = matches[1]
			pendingLine = matches[2]
			pendingMessage = matches[3]
			continue
		}

		// Check for stack trace start (happens before FAIL line in Go output)
		if stackTracePattern.MatchString(line) {
			inStackTrace = true
			pendingStackTrace.WriteString(line)
			pendingStackTrace.WriteString("\n")
			continue
		}

		// Continue collecting stack trace lines
		if inStackTrace {
			// Stop at empty line or next section marker
			if line == "" || strings.HasPrefix(line, "===") || strings.HasPrefix(line, "---") {
				inStackTrace = false
			} else {
				pendingStackTrace.WriteString(line)
				pendingStackTrace.WriteString("\n")
				continue
			}
		}

		// Check for FAIL line
		if matches := failLinePattern.FindStringSubmatch(line); matches != nil {
			// Save any previous failure
			if currentFailure != nil {
				failures = append(failures, *currentFailure)
			}

			// Start new failure with test name from FAIL line
			currentFailure = &TestFailure{
				TestName: matches[1],
			}

			// If we had pending file:line info, add it now
			if pendingFile != "" {
				currentFailure.FilePath = pendingFile
				lineNum, _ := strconv.Atoi(pendingLine)
				currentFailure.LineNumber = lineNum
				currentFailure.ErrorMessage = pendingMessage
				// Clear pending info
				pendingFile = ""
				pendingLine = ""
				pendingMessage = ""
			}

			// If we collected a stack trace, add it now
			if pendingStackTrace.Len() > 0 {
				currentFailure.StackTrace = strings.TrimSpace(pendingStackTrace.String())
				pendingStackTrace.Reset()
			}
			continue
		}
	}

	// Don't forget the last failure
	if currentFailure != nil {
		failures = append(failures, *currentFailure)
	}

	if err := scanner.Err(); err != nil {
		return failures, err
	}

	// Return empty slice if no failures found (not an error)
	if failures == nil {
		failures = []TestFailure{}
	}

	return failures, nil
}
