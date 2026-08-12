package testutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestReadTestOutput_Success(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a sample test output file
	testContent := []byte("Sample test output content\nLine 2\nLine 3")
	testFile := filepath.Join(tmpDir, "sample_output.txt")
	if err := os.WriteFile(testFile, testContent, 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Read the file
	data, err := ReadTestOutput(testFile)
	if err != nil {
		t.Fatalf("ReadTestOutput failed: %v", err)
	}

	// Verify the content matches
	if string(data) != string(testContent) {
		t.Errorf("Content mismatch: got %q, want %q", string(data), string(testContent))
	}
}

func TestReadTestOutput_FileNotFound(t *testing.T) {
	// Try to read a non-existent file
	nonExistentPath := "/path/that/does/not/exist.txt"
	_, err := ReadTestOutput(nonExistentPath)

	if err == nil {
		t.Fatal("Expected error for non-existent file, got nil")
	}

	// Verify it's a ErrFileNotFound
	if !errors.Is(err, ErrFileNotFound) {
		t.Errorf("Expected ErrFileNotFound, got: %v", err)
	}

	// Verify the error message contains the path
	errorMsg := err.Error()
	if filepath.Base(nonExistentPath) != filepath.Base(errorMsg) {
		t.Errorf("Error message should contain the file path, got: %s", errorMsg)
	}
}

func TestReadTestOutput_EmptyFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create an empty test output file
	emptyFile := filepath.Join(tmpDir, "empty_output.txt")
	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty test file: %v", err)
	}

	// Try to read the empty file
	_, err := ReadTestOutput(emptyFile)

	if err == nil {
		t.Fatal("Expected error for empty file, got nil")
	}

	// Verify it's a ErrEmptyFile
	if !errors.Is(err, ErrEmptyFile) {
		t.Errorf("Expected ErrEmptyFile, got: %v", err)
	}
}

func TestReadTestOutput_ReadPermissions(t *testing.T) {
	// Skip on Windows as permission handling is different
	// This test is most meaningful on Unix-like systems
	// Create a temporary directory for test files
	tmpDir := t.TempDir()

	// Create a file with no read permissions
	noReadFile := filepath.Join(tmpDir, "no_read_output.txt")
	if err := os.WriteFile(noReadFile, []byte("content"), 0000); err != nil {
		t.Fatalf("Failed to create test file with no permissions: %v", err)
	}

	// Try to read the file with no permissions
	_, err := ReadTestOutput(noReadFile)

	// We expect either a permission error during os.Stat or os.ReadFile
	// The exact behavior depends on the OS and whether we're running as root
	if err == nil {
		// If running as root, we might be able to read the file anyway
		t.Skip("Running as root or with elevated permissions, skipping permission test")
	}

	// Verify we get some kind of error
	if err != nil {
		// Check if the error message mentions permission
		errorMsg := err.Error()
		t.Logf("Got expected error for no-permission file: %v", err)
		if filepath.Base(noReadFile) != filepath.Base(errorMsg) {
			t.Logf("Error message: %s", errorMsg)
		}
	}
}

func TestReadTestOutput_DirectoryPath(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Try to read a directory as if it were a file
	_, err := ReadTestOutput(tmpDir)

	if err == nil {
		t.Fatal("Expected error when reading directory, got nil")
	}

	t.Logf("Got expected error for directory path: %v", err)
}
