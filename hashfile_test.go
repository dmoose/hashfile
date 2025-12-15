package hashfile

import (
	"bytes"
	"os"
	"testing"
)

// TestBasicProcessAndVerify tests the basic functionality of adding and verifying integrity comments
func TestBasicProcessAndVerify(t *testing.T) {
	tests := []struct {
		name    string
		content string
		style   CommentStyle
		wantErr bool
	}{
		{
			name:    "simple go file",
			content: "package main\n\nfunc main() {\n}\n",
			style:   GoStyle,
			wantErr: false,
		},
		{
			name:    "python file",
			content: "def hello():\n    print('world')\n",
			style:   PythonStyle,
			wantErr: false,
		},
		{
			name:    "file with no trailing newline",
			content: "package main\n\nfunc main() {\n}",
			style:   GoStyle,
			wantErr: false,
		},
		{
			name:    "empty file",
			content: "",
			style:   GoStyle,
			wantErr: false,
		},
		{
			name:    "single line",
			content: "package main\n",
			style:   GoStyle,
			wantErr: false,
		},
		{
			name:    "CRLF line endings",
			content: "package main\r\n\r\nfunc main() {\r\n}\r\n",
			style:   GoStyle,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpfile, err := os.CreateTemp("", "test_*.go")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpfile.Name())

			// Write test content
			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatal(err)
			}
			tmpfile.Close()

			// Process file
			config := Config{CommentStyle: tt.style, BufferSize: 64 * 1024}
			writer := NewWriter(config)
			err = writer.ProcessFile(tmpfile.Name())

			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			// Verify file
			reader := NewReader(config)
			valid, err := reader.VerifyFile(tmpfile.Name())
			if err != nil {
				t.Errorf("VerifyFile() error = %v", err)
				return
			}

			if !valid {
				t.Error("VerifyFile() returned false, expected true")
			}

			// Read file and check that comment was added
			content, err := os.ReadFile(tmpfile.Name())
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Contains(content, []byte("FileIntegrity:")) {
				t.Error("File does not contain integrity comment")
			}
		})
	}
}

// TestIdempotency ensures that processing a file twice doesn't change it
func TestIdempotency(t *testing.T) {
	content := "package main\n\nfunc main() {\n}\n"

	// Create temp file
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())

	// Process first time
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("First ProcessFile() failed: %v", err)
	}

	// Get file info after first process
	info1, err := os.Stat(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}
	content1, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Process second time
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("Second ProcessFile() failed: %v", err)
	}

	// Get file info after second process
	info2, err := os.Stat(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}
	content2, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Content should be identical
	if !bytes.Equal(content1, content2) {
		t.Error("File content changed on second process")
	}

	// Modification time should be identical (file wasn't touched)
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("File modification time changed on second process (file should not have been modified)")
	}
}

// TestUpdateWhenContentChanges ensures that changing file content updates the hash
func TestUpdateWhenContentChanges(t *testing.T) {
	// Create temp file
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Write initial content
	initialContent := "package main\n\nfunc main() {\n}\n"
	if _, err := tmpfile.Write([]byte(initialContent)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())
	reader := NewReader(DefaultConfig())

	// Process first time
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("First ProcessFile() failed: %v", err)
	}

	// Verify
	valid, err := reader.VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("VerifyFile() failed: %v", err)
	}
	if !valid {
		t.Error("Initial verification failed")
	}

	// Read file and manually modify content (simulating user edit)
	content, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Remove the hash comment and add new code
	lines := bytes.Split(content, []byte("\n"))
	var newContent []byte
	for _, line := range lines {
		if !bytes.Contains(line, []byte("FileIntegrity:")) {
			newContent = append(newContent, line...)
			newContent = append(newContent, '\n')
		}
	}
	newContent = append(newContent, []byte("func hello() {}\n")...)

	// Write modified content
	if err := os.WriteFile(tmpfile.Name(), newContent, 0644); err != nil {
		t.Fatal(err)
	}

	// Process again
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("Second ProcessFile() failed: %v", err)
	}

	// Verify new content
	valid, err = reader.VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("Second VerifyFile() failed: %v", err)
	}
	if !valid {
		t.Error("Verification after content change failed")
	}

	// Ensure new function is still there
	finalContent, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(finalContent, []byte("func hello()")) {
		t.Error("New function disappeared after processing")
	}
}

// TestVerifyDetectsModification ensures that verification fails when content is modified
func TestVerifyDetectsModification(t *testing.T) {
	content := "package main\n\nfunc main() {\n}\n"

	// Create temp file
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())
	reader := NewReader(DefaultConfig())

	// Process file
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("ProcessFile() failed: %v", err)
	}

	// Read and modify content
	fileContent, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	// Insert a character in the middle (before the hash comment)
	modified := bytes.Replace(fileContent, []byte("func main()"), []byte("func main2()"), 1)
	if err := os.WriteFile(tmpfile.Name(), modified, 0644); err != nil {
		t.Fatal(err)
	}

	// Verify should fail
	valid, err := reader.VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("VerifyFile() returned error: %v", err)
	}
	if valid {
		t.Error("VerifyFile() returned true for modified file, expected false")
	}
}

// TestLineEndingPreservation ensures that line endings are preserved
func TestLineEndingPreservation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantLF  bool // true for LF, false for CRLF
	}{
		{
			name:    "LF endings",
			content: "line1\nline2\nline3\n",
			wantLF:  true,
		},
		{
			name:    "CRLF endings",
			content: "line1\r\nline2\r\nline3\r\n",
			wantLF:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp("", "test_*.txt")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatal(err)
			}
			tmpfile.Close()

			config := Config{CommentStyle: GoStyle, BufferSize: 64 * 1024}
			writer := NewWriter(config)
			if err := writer.ProcessFile(tmpfile.Name()); err != nil {
				t.Fatalf("ProcessFile() failed: %v", err)
			}

			// Read result
			result, err := os.ReadFile(tmpfile.Name())
			if err != nil {
				t.Fatal(err)
			}

			// Find the integrity comment line
			if !bytes.Contains(result, []byte("FileIntegrity:")) {
				t.Fatal("Integrity comment not found")
			}

			// Check line ending of the comment
			if tt.wantLF {
				if !bytes.Contains(result, []byte("FileIntegrity:")) {
					t.Fatal("Comment not found")
				}
				// Should have LF, not CRLF before the hash line
				if bytes.Contains(result, []byte("\r\nFileIntegrity:")) {
					t.Error("Found CRLF in LF file")
				}
			} else {
				// Should have CRLF
				lines := bytes.Split(result, []byte("\n"))
				for _, line := range lines {
					if bytes.Contains(line, []byte("FileIntegrity:")) {
						if len(line) > 0 && line[len(line)-1] != '\r' {
							t.Error("Expected CRLF line ending, got LF")
						}
					}
				}
			}
		})
	}
}

// TestDifferentCommentStyles tests various comment styles
func TestDifferentCommentStyles(t *testing.T) {
	tests := []struct {
		name    string
		style   CommentStyle
		content string
	}{
		{
			name:    "Go style",
			style:   GoStyle,
			content: "package main\n",
		},
		{
			name:    "Python style",
			style:   PythonStyle,
			content: "def main():\n    pass\n",
		},
		{
			name:    "SQL style",
			style:   SQLStyle,
			content: "SELECT * FROM users;\n",
		},
		{
			name:    "HTML style",
			style:   HTMLStyle,
			content: "<html><body></body></html>\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpfile, err := os.CreateTemp("", "test_*")
			if err != nil {
				t.Fatal(err)
			}
			defer os.Remove(tmpfile.Name())

			if _, err := tmpfile.Write([]byte(tt.content)); err != nil {
				t.Fatal(err)
			}
			tmpfile.Close()

			config := Config{CommentStyle: tt.style, BufferSize: 64 * 1024}
			writer := NewWriter(config)
			reader := NewReader(config)

			if err := writer.ProcessFile(tmpfile.Name()); err != nil {
				t.Fatalf("ProcessFile() failed: %v", err)
			}

			valid, err := reader.VerifyFile(tmpfile.Name())
			if err != nil {
				t.Fatalf("VerifyFile() failed: %v", err)
			}
			if !valid {
				t.Error("VerifyFile() returned false")
			}

			// Check that the correct comment style was used
			content, err := os.ReadFile(tmpfile.Name())
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Contains(content, []byte(tt.style.Prefix+"FileIntegrity:")) {
				t.Errorf("Comment does not contain expected prefix %q", tt.style.Prefix)
			}
		})
	}
}

// TestConfigForExtension tests auto-detection of comment styles
func TestConfigForExtension(t *testing.T) {
	tests := []struct {
		ext       string
		wantStyle CommentStyle
	}{
		{".go", GoStyle},
		{".py", PythonStyle},
		{".sql", SQLStyle},
		{".html", HTMLStyle},
		{".c", CStyle},
		{".cpp", CStyle},
		{".java", CStyle},
		{".js", CStyle},
		{".sh", ShellStyle},
		{".rb", RubyStyle},
		{".unknown", GoStyle}, // default
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			config := ConfigForExtension(tt.ext)
			if config.CommentStyle != tt.wantStyle {
				t.Errorf("ConfigForExtension(%q) = %v, want %v", tt.ext, config.CommentStyle, tt.wantStyle)
			}
		})
	}
}

// TestFilePermissions ensures file permissions are preserved
func TestFilePermissions(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := "package main\n\nfunc main() {}\n"
	if _, err := tmpfile.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	// Set specific permissions
	if err := os.Chmod(tmpfile.Name(), 0600); err != nil {
		t.Fatal(err)
	}

	// Get original permissions
	info1, err := os.Stat(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}
	origMode := info1.Mode()

	// Process file
	writer := NewWriter(DefaultConfig())
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("ProcessFile() failed: %v", err)
	}

	// Check permissions
	info2, err := os.Stat(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}
	newMode := info2.Mode()

	if origMode != newMode {
		t.Errorf("File permissions changed from %v to %v", origMode, newMode)
	}
}

// TestLargeFile tests processing of larger files to ensure streaming works
func TestLargeFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Create a file larger than the buffer size
	content := []byte("package main\n\n")
	for i := 0; i < 10000; i++ {
		content = append(content, []byte("// This is a comment line\n")...)
	}
	content = append(content, []byte("func main() {}\n")...)

	if _, err := tmpfile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())
	reader := NewReader(DefaultConfig())

	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("ProcessFile() failed: %v", err)
	}

	valid, err := reader.VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("VerifyFile() failed: %v", err)
	}
	if !valid {
		t.Error("VerifyFile() returned false for large file")
	}
}

// TestNoTrailingNewline ensures we handle files without trailing newlines
func TestNoTrailingNewline(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	// Content without trailing newline
	content := []byte("package main\n\nfunc main() {}")
	if _, err := tmpfile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())
	reader := NewReader(DefaultConfig())

	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("ProcessFile() failed: %v", err)
	}

	valid, err := reader.VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("VerifyFile() failed: %v", err)
	}
	if !valid {
		t.Error("VerifyFile() returned false")
	}

	// Verify the comment is on its own line
	result, err := os.ReadFile(tmpfile.Name())
	if err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(result, []byte("\n"))
	foundComment := false
	for _, line := range lines {
		if bytes.Contains(line, []byte("FileIntegrity:")) {
			foundComment = true
			// Check it's on its own line (not appended to existing content)
			if bytes.Contains(line, []byte("}")) {
				t.Error("Comment was appended to existing line instead of being on its own line")
			}
		}
	}
	if !foundComment {
		t.Error("Integrity comment not found")
	}
}

// TestConvenienceFunctions tests the package-level convenience functions
func TestConvenienceFunctions(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := []byte("package main\n\nfunc main() {}\n")
	if _, err := tmpfile.Write(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	// Test ProcessFile
	if err := ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("ProcessFile() failed: %v", err)
	}

	// Test VerifyFile
	valid, err := VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("VerifyFile() failed: %v", err)
	}
	if !valid {
		t.Error("VerifyFile() returned false")
	}
}

// TestEmptyFile tests processing of empty files
func TestEmptyFile(t *testing.T) {
	tmpfile, err := os.CreateTemp("", "test_*.go")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())
	reader := NewReader(DefaultConfig())

	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		t.Fatalf("ProcessFile() failed on empty file: %v", err)
	}

	valid, err := reader.VerifyFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("VerifyFile() failed on empty file: %v", err)
	}
	if !valid {
		t.Error("VerifyFile() returned false for empty file")
	}
}

// BenchmarkProcessFile benchmarks file processing
func BenchmarkProcessFile(b *testing.B) {
	// Create a temporary file
	tmpfile, err := os.CreateTemp("", "bench_*.go")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := []byte("package main\n\n")
	for i := 0; i < 1000; i++ {
		content = append(content, []byte("// Comment line\n")...)
	}
	content = append(content, []byte("func main() {}\n")...)

	if _, err := tmpfile.Write(content); err != nil {
		b.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Remove hash before each iteration
		if err := os.WriteFile(tmpfile.Name(), content, 0644); err != nil {
			b.Fatal(err)
		}
		if err := writer.ProcessFile(tmpfile.Name()); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVerifyFile benchmarks file verification
func BenchmarkVerifyFile(b *testing.B) {
	// Create and process a file
	tmpfile, err := os.CreateTemp("", "bench_*.go")
	if err != nil {
		b.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	content := []byte("package main\n\n")
	for i := 0; i < 1000; i++ {
		content = append(content, []byte("// Comment line\n")...)
	}
	content = append(content, []byte("func main() {}\n")...)

	if _, err := tmpfile.Write(content); err != nil {
		b.Fatal(err)
	}
	tmpfile.Close()

	writer := NewWriter(DefaultConfig())
	if err := writer.ProcessFile(tmpfile.Name()); err != nil {
		b.Fatal(err)
	}

	reader := NewReader(DefaultConfig())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := reader.VerifyFile(tmpfile.Name()); err != nil {
			b.Fatal(err)
		}
	}
}
