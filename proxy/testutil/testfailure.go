package testutil

import (
	"bufio"
	"bytes"
	"log"
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
// - Parallel test output (intermixed RUN/PASS/FAIL lines)
// - t.Helper() failures (nested stack frames)
// - Malformed or incomplete output (logs warnings, continues parsing)
//
// Parameters:
//   - output: The raw test output bytes
//
// Returns:
//   - []TestFailure: List of parsed test failures (empty if none found)
//   - error: An error if parsing fails catastrophically (not for partial parsing)
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
	// Handle empty or nil input gracefully
	if len(output) == 0 {
		return []TestFailure{}, nil
	}

	var failures []TestFailure
	var currentFailure *TestFailure
	var pendingFile, pendingLine, pendingMessage string
	var pendingStackTrace strings.Builder
	var inStackTrace bool
	var currentTestName string
	var lineNum int

	scanner := bufio.NewScanner(bytes.NewReader(output))
	// Increase buffer size for long lines (e.g., stack traces)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	// Regex patterns
	// Matches: --- FAIL: TestName (0.05s) or --- FAIL: TestName
	failLinePattern := regexp.MustCompile(`^--- FAIL:\s+(\S+)`)
	// Matches: === RUN   TestName
	runLinePattern := regexp.MustCompile(`^=== RUN\s+(\S+)`)
	// Matches: --- PASS: TestName (0.05s)
	passLinePattern := regexp.MustCompile(`^--- PASS:\s+(\S+)`)
	// Matches:    file_test.go:45: error message
	fileLinePattern := regexp.MustCompile(`^\s+(\S+\.go):(\d+):\s*(.*)`)
	// Matches: goroutine N [running]: (or other states)
	// Allows leading whitespace/tabs
	stackTracePattern := regexp.MustCompile(`^\s*goroutine \d+\s+\[`)

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip completely empty lines early
		if line == "" {
			continue
		}

		// Track current test being run (for parallel test disambiguation)
		if matches := runLinePattern.FindStringSubmatch(line); matches != nil {
			currentTestName = matches[1]
			continue
		}

		// Handle PASS lines - clear state for passed tests
		if matches := passLinePattern.FindStringSubmatch(line); matches != nil {
			// If we have pending info for a test that passed, discard it
			if currentTestName == matches[1] {
				pendingFile = ""
				pendingLine = ""
				pendingMessage = ""
				pendingStackTrace.Reset()
				inStackTrace = false
			}
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
		// Stack traces end when we see a FAIL line or an empty line
		if inStackTrace {
			// Check if this is a FAIL line - stop collecting and process it below
			if failLinePattern.MatchString(line) || strings.HasPrefix(line, "--- FAIL:") {
				inStackTrace = false
				// Continue to process the FAIL line below
			} else if line == "" {
				// Empty line ends the stack trace section
				inStackTrace = false
				continue
			} else {
				// Still in stack trace - collect this line
				pendingStackTrace.WriteString(line)
				pendingStackTrace.WriteString("\n")
				continue
			}
		}

		// Check for file:line pattern (comes before FAIL line in Go output)
		// Use a more lenient pattern first: file:something (even if not a number)
		if matches := fileLinePattern.FindStringSubmatch(line); matches != nil {
			pendingFile = matches[1]
			pendingLine = matches[2]
			pendingMessage = matches[3]
			continue
		}

		// Try a more lenient pattern for malformed line numbers: file:non-number: message
		lenientPattern := regexp.MustCompile(`^\s+(\S+\.go):\s*([^\s:]+):\s*(.*)`)
		if matches := lenientPattern.FindStringSubmatch(line); matches != nil && pendingFile == "" {
			pendingFile = matches[1]
			pendingLine = matches[2]
			pendingMessage = matches[3]
			continue
		}

		// Check for FAIL line
		if matches := failLinePattern.FindStringSubmatch(line); matches != nil {
			testName := matches[1]

			// Save any previous failure
			if currentFailure != nil {
				failures = append(failures, *currentFailure)
				currentFailure = nil
			}

			// Start new failure with test name from FAIL line
			currentFailure = &TestFailure{
				TestName: testName,
			}

			// If we had pending file:line info, add it now
			if pendingFile != "" {
				currentFailure.FilePath = pendingFile

				// Parse line number with error handling
				if lineNum, err := strconv.Atoi(pendingLine); err == nil {
					currentFailure.LineNumber = lineNum
				} else {
					// Log warning but don't fail - keep line number as 0
					log.Printf("Warning: Failed to parse line number %q at line %d: %v", pendingLine, lineNum, err)
				}

				currentFailure.ErrorMessage = pendingMessage
			} else {
				// No file:line info found - this is unusual but not fatal
				log.Printf("Warning: No file:line information found for test failure %s at line %d", testName, lineNum)
			}

			// If we collected a stack trace, add it now
			if pendingStackTrace.Len() > 0 {
				currentFailure.StackTrace = strings.TrimSpace(pendingStackTrace.String())
			}

			// Clear pending state for next failure
			pendingFile = ""
			pendingLine = ""
			pendingMessage = ""
			pendingStackTrace.Reset()
			inStackTrace = false
			currentTestName = ""
			continue
		}
	}

	// Don't forget the last failure
	if currentFailure != nil {
		failures = append(failures, *currentFailure)
	}

	// Check for scanner errors (e.g., buffer overflow)
	if err := scanner.Err(); err != nil {
		// Log the error but return what we have so far
		log.Printf("Warning: Scanner error while parsing test output at line %d: %v", lineNum, err)
		return failures, err
	}

	// Return empty slice if no failures found (not an error)
	if failures == nil {
		failures = []TestFailure{}
	}

	return failures, nil
}
