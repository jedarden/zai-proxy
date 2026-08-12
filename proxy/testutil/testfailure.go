package testutil

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
