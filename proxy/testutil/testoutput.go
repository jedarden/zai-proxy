package testutil

import (
	"errors"
	"fmt"
	"os"
)

var (
	// ErrFileNotFound is returned when the test output file does not exist
	ErrFileNotFound = errors.New("test output file not found")

	// ErrEmptyFile is returned when the test output file is empty
	ErrEmptyFile = errors.New("test output file is empty")
)

// ReadTestOutput reads the raw bytes from a test output file.
// It performs basic validation to ensure the file exists, is readable, and is not empty.
//
// Parameters:
//   - path: The file system path to the test output file
//
// Returns:
//   - []byte: The raw contents of the test output file
//   - error: An error if the file cannot be read or is empty
//
// Example usage:
//   data, err := testutil.ReadTestOutput("testdata/sample_output.txt")
//   if err != nil {
//       log.Fatalf("Failed to read test output: %v", err)
//   }
func ReadTestOutput(path string) ([]byte, error) {
	// Check if file exists and get its info
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileNotFound, path)
		}
		// Check for permission errors
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied reading file: %s", path)
		}
		return nil, fmt.Errorf("failed to access file %s: %w", path, err)
	}

	// Validate file is not empty
	if info.Size() == 0 {
		return nil, fmt.Errorf("%w: %s", ErrEmptyFile, path)
	}

	// Read the file contents
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsPermission(err) {
			return nil, fmt.Errorf("permission denied reading file: %s", path)
		}
		return nil, fmt.Errorf("failed to read file %s: %w", path, err)
	}

	return data, nil
}
