// Package hashfile provides efficient streaming CRC32-based file integrity checking.
// It adds a comment line at the end of files containing a CRC32 checksum of the file content
// (excluding the comment itself). This allows detection of changes to source code files.
//
// The package uses a streaming algorithm with a single buffer allocation and sliding window,
// making it efficient for files of any size. It preserves file attributes (permissions, ownership)
// and only modifies files when the integrity comment is missing or incorrect.
package hashfile

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

// CommentStyle defines the comment format for different programming languages.
type CommentStyle struct {
	Prefix string // Comment prefix (e.g., "// " for Go/C)
	Suffix string // Comment suffix (e.g., " -->" for HTML, empty for most)
}

// Predefined comment styles for common languages.
var (
	GoStyle     = CommentStyle{Prefix: "// ", Suffix: ""}
	CStyle      = CommentStyle{Prefix: "// ", Suffix: ""}
	PythonStyle = CommentStyle{Prefix: "# ", Suffix: ""}
	SQLStyle    = CommentStyle{Prefix: "-- ", Suffix: ""}
	HTMLStyle   = CommentStyle{Prefix: "<!-- ", Suffix: " -->"}
	ShellStyle  = CommentStyle{Prefix: "# ", Suffix: ""}
	RubyStyle   = CommentStyle{Prefix: "# ", Suffix: ""}
	JSStyle     = CommentStyle{Prefix: "// ", Suffix: ""}
)

// Config holds processing configuration.
type Config struct {
	CommentStyle CommentStyle
	BufferSize   int // Buffer size for streaming (default 64KB)
}

// DefaultConfig returns configuration with Go-style comments and standard buffer size.
func DefaultConfig() Config {
	return Config{
		CommentStyle: GoStyle,
		BufferSize:   64 * 1024, // 64KB buffer
	}
}

// ConfigForExtension returns a Config with appropriate comment style for the given file extension.
// Returns DefaultConfig for unknown extensions.
func ConfigForExtension(ext string) Config {
	config := DefaultConfig()

	switch ext {
	case ".go":
		config.CommentStyle = GoStyle
	case ".c", ".h", ".cpp", ".hpp", ".cc", ".cxx", ".java", ".js", ".ts", ".jsx", ".tsx":
		config.CommentStyle = CStyle
	case ".py":
		config.CommentStyle = PythonStyle
	case ".sql":
		config.CommentStyle = SQLStyle
	case ".html", ".htm", ".xml":
		config.CommentStyle = HTMLStyle
	case ".sh", ".bash":
		config.CommentStyle = ShellStyle
	case ".rb":
		config.CommentStyle = RubyStyle
	}

	return config
}

// maxCommentSize calculates the maximum possible size of an integrity comment.
// Format: "prefix + FileIntegrity: + 8hex + suffix + CRLF"
func (c Config) maxCommentSize() int {
	return len(c.CommentStyle.Prefix) + len("FileIntegrity: ") + 8 + len(c.CommentStyle.Suffix) + 2
}

// Writer processes files using efficient streaming algorithm.
type Writer struct {
	config  Config
	pattern *regexp.Regexp // Pre-compiled pattern for performance
}

// NewWriter creates a Writer with the given configuration.
func NewWriter(config Config) *Writer {
	return &Writer{
		config:  config,
		pattern: createCommentPattern(config.CommentStyle),
	}
}

// ProcessFile adds or updates the integrity comment in a file.
// It uses a streaming algorithm to minimize memory usage and only modifies
// the file if the integrity comment is missing or incorrect.
// File attributes (permissions, ownership) are preserved.
func (w *Writer) ProcessFile(filename string) error {
	// Get original file info for attribute preservation
	origInfo, err := os.Stat(filename)
	if err != nil {
		return fmt.Errorf("failed to stat source file: %w", err)
	}

	// Open source file
	src, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer src.Close()

	// Create temporary output file in same directory for atomic replacement
	dir := filepath.Dir(filename)
	dst, err := os.CreateTemp(dir, ".hashfile_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := dst.Name()

	// Ensure cleanup on error
	var success bool
	defer func() {
		dst.Close()
		if !success {
			os.Remove(tmpName)
		}
	}()

	// Process stream - returns true if no-op (existing CRC matches calculated CRC)
	isNoOp, err := w.processStream(src, dst)
	if err != nil {
		return fmt.Errorf("failed to process stream: %w", err)
	}

	// Close files
	src.Close()
	dst.Close()

	if isNoOp {
		// File already has correct hash - no-op, delete temp file
		os.Remove(tmpName)
		success = true
		return nil
	}

	// Preserve file attributes
	if err := preserveAttributes(tmpName, origInfo); err != nil {
		return fmt.Errorf("failed to preserve attributes: %w", err)
	}

	// Atomic replace
	if err := os.Rename(tmpName, filename); err != nil {
		return fmt.Errorf("failed to replace file: %w", err)
	}

	success = true
	return nil
}

// processStream implements the efficient sliding window algorithm.
// Returns true if no-op (file already has correct hash), false if file was modified.
func (w *Writer) processStream(src io.Reader, dst io.Writer) (bool, error) {
	windowSize := w.config.maxCommentSize() + 2 // +2 for potential CRLF before comment
	buffer := make([]byte, w.config.BufferSize) // Single allocation

	hasher := crc32.NewIEEE()
	writer := bufio.NewWriter(dst)
	defer writer.Flush()

	// First read - fill entire buffer
	n, err := src.Read(buffer)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read error: %w", err)
	}

	if n == 0 {
		// Empty file - just add comment
		if err := w.finalizeEmpty(writer, hasher); err != nil {
			return false, err
		}
		return false, nil // Empty file always needs hash added
	}

	firstRead := true
	eof := (err == io.EOF)

	for !eof {
		if firstRead {
			// First read: CRC and write everything except last windowSize bytes
			if n > windowSize {
				writeLen := n - windowSize
				if _, err := writer.Write(buffer[:writeLen]); err != nil {
					return false, fmt.Errorf("write error: %w", err)
				}
				hasher.Write(buffer[:writeLen])

				// Slide window to start of buffer
				copy(buffer, buffer[writeLen:n])
				n = n - writeLen
			}
			firstRead = false
		} else {
			// Subsequent reads: CRC and write everything in buffer before window
			// (the window is at buffer[0:n] from previous iteration)
			if _, err := writer.Write(buffer[:n-windowSize]); err != nil {
				return false, fmt.Errorf("write error: %w", err)
			}
			hasher.Write(buffer[:n-windowSize])

			// Slide window to start
			copy(buffer, buffer[n-windowSize:n])
			n = windowSize
		}

		// Read more data starting after the window
		bytesRead, err := src.Read(buffer[n:])
		if err != nil && err != io.EOF {
			return false, fmt.Errorf("read error: %w", err)
		}
		n += bytesRead
		eof = (err == io.EOF)
	}

	// At EOF: buffer[0:n] contains the last bytes of the file (the window)
	return w.finalizeWindow(writer, hasher, buffer[:n])
}

// finalizeEmpty handles empty files.
func (w *Writer) finalizeEmpty(writer *bufio.Writer, hasher hash.Hash32) error {
	crc := hasher.Sum32()
	lineEnding := "\n"
	comment := w.createComment(crc, lineEnding)

	if _, err := writer.Write(comment); err != nil {
		return fmt.Errorf("write error: %w", err)
	}
	return nil
}

// finalizeWindow processes the final window at EOF.
// Returns true if no-op (existing CRC matches calculated CRC), false if file needs update.
func (w *Writer) finalizeWindow(writer *bufio.Writer, hasher hash.Hash32, window []byte) (bool, error) {
	// Check if there's an existing integrity comment in the window
	match := w.pattern.FindSubmatchIndex(window)

	var contentPart []byte
	var existingCRC uint32
	var hasExistingComment bool

	if match != nil {
		// Found existing comment - content is everything before it
		contentPart = window[:match[0]]

		// Parse the existing CRC
		crcHex := window[match[2]:match[3]]
		crcBytes, err := hex.DecodeString(string(crcHex))
		if err == nil && len(crcBytes) == 4 {
			existingCRC = uint32(crcBytes[0])<<24 | uint32(crcBytes[1])<<16 |
				uint32(crcBytes[2])<<8 | uint32(crcBytes[3])
			hasExistingComment = true
		}
	} else {
		// No existing comment - all of window is content
		contentPart = window
	}

	// Detect line ending style from content
	lineEnding := detectLineEnding(window)

	// CRC the content part (excluding trailing newline if present)
	crcContent := contentPart
	needsNewline := false

	if len(crcContent) > 0 {
		// Check if content ends with newline
		if crcContent[len(crcContent)-1] == '\n' {
			// Has trailing newline - CRC it, then strip for writing check
			if len(crcContent) > 1 && crcContent[len(crcContent)-2] == '\r' {
				// CRLF ending
				hasher.Write(crcContent[:len(crcContent)-2])
			} else {
				// LF ending
				hasher.Write(crcContent[:len(crcContent)-1])
			}
		} else {
			// No trailing newline - CRC all content, and we'll need to add one
			hasher.Write(crcContent)
			needsNewline = true
		}
	}

	// Calculate final CRC
	calculatedCRC := hasher.Sum32()

	// If we have an existing comment with the same CRC, this is a no-op
	if hasExistingComment && calculatedCRC == existingCRC {
		// File already has correct hash - signal no-op
		// Still write to temp file for consistency, but signal caller to skip replace
		if _, err := writer.Write(window); err != nil {
			return false, fmt.Errorf("write error: %w", err)
		}
		return true, nil
	}

	// Write the content part
	if _, err := writer.Write(contentPart); err != nil {
		return false, fmt.Errorf("write error: %w", err)
	}

	// Add newline if content doesn't end with one
	if needsNewline {
		if _, err := writer.Write([]byte(lineEnding)); err != nil {
			return false, fmt.Errorf("write error: %w", err)
		}
	}

	// Write new comment with calculated CRC
	comment := w.createComment(calculatedCRC, lineEnding)
	if _, err := writer.Write(comment); err != nil {
		return false, fmt.Errorf("write error: %w", err)
	}

	return false, nil // File was modified
}

// createComment generates the integrity comment with proper line ending.
func (w *Writer) createComment(crc uint32, lineEnding string) []byte {
	comment := fmt.Sprintf("%sFileIntegrity: %08X%s%s",
		w.config.CommentStyle.Prefix,
		crc,
		w.config.CommentStyle.Suffix,
		lineEnding)
	return []byte(comment)
}

// Reader verifies file integrity using the same efficient streaming approach.
type Reader struct {
	config  Config
	pattern *regexp.Regexp
}

// NewReader creates a Reader with the given configuration.
func NewReader(config Config) *Reader {
	return &Reader{
		config:  config,
		pattern: createCommentPattern(config.CommentStyle),
	}
}

// VerifyFile checks if a file's integrity comment matches its content.
func (r *Reader) VerifyFile(filename string) (bool, error) {
	file, err := os.Open(filename)
	if err != nil {
		return false, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return r.verifyStream(file)
}

// verifyStream implements streaming verification with same sliding window algorithm.
func (r *Reader) verifyStream(src io.Reader) (bool, error) {
	windowSize := r.config.maxCommentSize() + 2
	buffer := make([]byte, r.config.BufferSize)

	hasher := crc32.NewIEEE()

	// First read
	n, err := src.Read(buffer)
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("read error: %w", err)
	}

	if n == 0 {
		return false, fmt.Errorf("empty file")
	}

	firstRead := true
	eof := (err == io.EOF)

	for !eof {
		if firstRead {
			// First read: CRC everything except last windowSize bytes
			if n > windowSize {
				hashLen := n - windowSize
				hasher.Write(buffer[:hashLen])

				// Slide window to start
				copy(buffer, buffer[hashLen:n])
				n = n - hashLen
			}
			firstRead = false
		} else {
			// Subsequent reads: CRC everything before window
			hasher.Write(buffer[:n-windowSize])

			// Slide window to start
			copy(buffer, buffer[n-windowSize:n])
			n = windowSize
		}

		// Read more data
		bytesRead, err := src.Read(buffer[n:])
		if err != nil && err != io.EOF {
			return false, fmt.Errorf("read error: %w", err)
		}
		n += bytesRead
		eof = (err == io.EOF)
	}

	// At EOF: buffer[0:n] contains the final window
	return r.verifyWindow(hasher, buffer[:n])
}

// verifyWindow extracts and verifies the CRC from the final window.
func (r *Reader) verifyWindow(hasher hash.Hash32, window []byte) (bool, error) {
	// Find the integrity comment
	match := r.pattern.FindSubmatchIndex(window)
	if match == nil {
		return false, fmt.Errorf("no integrity comment found")
	}

	// Extract stored CRC
	crcHex := window[match[2]:match[3]]
	crcBytes, err := hex.DecodeString(string(crcHex))
	if err != nil || len(crcBytes) != 4 {
		return false, fmt.Errorf("invalid CRC format")
	}

	storedCRC := uint32(crcBytes[0])<<24 | uint32(crcBytes[1])<<16 |
		uint32(crcBytes[2])<<8 | uint32(crcBytes[3])

	// CRC the content before the comment (excluding trailing newline)
	contentPart := window[:match[0]]

	if len(contentPart) > 0 {
		// Strip trailing newline before CRCing
		if contentPart[len(contentPart)-1] == '\n' {
			if len(contentPart) > 1 && contentPart[len(contentPart)-2] == '\r' {
				contentPart = contentPart[:len(contentPart)-2]
			} else {
				contentPart = contentPart[:len(contentPart)-1]
			}
		}
		hasher.Write(contentPart)
	}

	calculatedCRC := hasher.Sum32()
	return calculatedCRC == storedCRC, nil
}

// Helper functions

// createCommentPattern creates a regex pattern for finding integrity comments.
func createCommentPattern(style CommentStyle) *regexp.Regexp {
	prefix := regexp.QuoteMeta(style.Prefix)
	suffix := regexp.QuoteMeta(style.Suffix)
	pattern := fmt.Sprintf(`(?m)^%sFileIntegrity: ([0-9A-F]{8})%s\r?\n?$`, prefix, suffix)
	return regexp.MustCompile(pattern)
}

// detectLineEnding detects whether the content uses CRLF or LF line endings.
func detectLineEnding(content []byte) string {
	// Scan for the first newline
	for i := 0; i < len(content); i++ {
		if content[i] == '\n' {
			if i > 0 && content[i-1] == '\r' {
				return "\r\n"
			}
			return "\n"
		}
	}
	// Default to LF if no newlines found
	return "\n"
}

// preserveAttributes copies file attributes from source to destination.
func preserveAttributes(dst string, srcInfo os.FileInfo) error {
	// Preserve permissions
	if err := os.Chmod(dst, srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to preserve permissions: %w", err)
	}

	// Preserve ownership (Unix-specific)
	if stat, ok := srcInfo.Sys().(*syscall.Stat_t); ok {
		// Ignore errors - we may not have rights to change ownership
		os.Chown(dst, int(stat.Uid), int(stat.Gid))
	}

	return nil
}

// Convenience functions for common operations.

// ProcessGoFile adds or updates integrity comment in a Go source file.
func ProcessGoFile(filename string) error {
	writer := NewWriter(DefaultConfig())
	return writer.ProcessFile(filename)
}

// VerifyGoFile verifies the integrity of a Go source file.
func VerifyGoFile(filename string) (bool, error) {
	reader := NewReader(DefaultConfig())
	return reader.VerifyFile(filename)
}

// ProcessFile adds or updates integrity comment with auto-detected comment style.
func ProcessFile(filename string) error {
	ext := filepath.Ext(filename)
	config := ConfigForExtension(ext)
	writer := NewWriter(config)
	return writer.ProcessFile(filename)
}

// VerifyFile verifies file integrity with auto-detected comment style.
func VerifyFile(filename string) (bool, error) {
	ext := filepath.Ext(filename)
	config := ConfigForExtension(ext)
	reader := NewReader(config)
	return reader.VerifyFile(filename)
}

// FileIntegrity: C11ECDCD
