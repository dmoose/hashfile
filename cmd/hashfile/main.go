// hashfile is a command-line tool for adding, verifying, and managing
// file integrity comments in source code files.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dmoose/hashfile"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "add":
		os.Exit(runAdd(os.Args[2:]))
	case "verify":
		os.Exit(runVerify(os.Args[2:]))
	case "check":
		os.Exit(runCheck(os.Args[2:]))
	case "version":
		fmt.Printf("hashfile version %s\n", version)
		os.Exit(0)
	case "help", "-h", "--help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `hashfile - File integrity verification tool

USAGE:
    hashfile <command> [options] <file>...

COMMANDS:
    add        Add or update integrity comments in files
    verify     Verify file integrity (exit 0 if valid, 1 if invalid)
    check      Check and display integrity status (human-readable)
    version    Show version information
    help       Show this help message

OPTIONS:
    -style     Comment style (go|python|c|sql|html|shell|ruby|js)
               Default: auto-detect from file extension

EXAMPLES:
    # Add integrity comments to Go files
    hashfile add main.go handler.go

    # Verify files (silent, use exit code)
    hashfile verify *.go

    # Check files with human-readable output
    hashfile check src/*.py

    # Use specific comment style
    hashfile add -style=python script.txt

EXIT CODES:
    0    Success (all files valid for verify, all operations succeeded)
    1    Failure (invalid files found or errors occurred)

`)
}

func runAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	style := fs.String("style", "", "Comment style (go|python|c|sql|html|shell|ruby|js)")
	fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no files specified\n")
		return 1
	}

	// Collect all files (expand globs if needed)
	allFiles, err := expandFiles(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	var errors []string
	successCount := 0

	for _, file := range allFiles {
		config := getConfig(file, *style)
		writer := hashfile.NewWriter(config)

		if err := writer.ProcessFile(file); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", file, err))
		} else {
			successCount++
		}
	}

	// Report results
	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		}
		fmt.Fprintf(os.Stderr, "\nProcessed %d files successfully, %d failed\n", successCount, len(errors))
		return 1
	}

	fmt.Printf("Successfully processed %d file(s)\n", successCount)
	return 0
}

func runVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	style := fs.String("style", "", "Comment style (go|python|c|sql|html|shell|ruby|js)")
	quiet := fs.Bool("q", false, "Quiet mode (no output, only exit code)")
	fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "Error: no files specified\n")
		}
		return 1
	}

	// Expand files
	allFiles, err := expandFiles(files)
	if err != nil {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		return 1
	}

	var errors []string
	var invalid []string
	validCount := 0

	for _, file := range allFiles {
		config := getConfig(file, *style)
		reader := hashfile.NewReader(config)

		valid, err := reader.VerifyFile(file)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", file, err))
		} else if !valid {
			invalid = append(invalid, file)
		} else {
			validCount++
		}
	}

	// Report results in quiet mode or verbose mode
	if !*quiet {
		if len(errors) > 0 {
			for _, err := range errors {
				fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			}
		}
		if len(invalid) > 0 {
			for _, file := range invalid {
				fmt.Fprintf(os.Stderr, "Invalid: %s\n", file)
			}
		}
	}

	if len(errors) > 0 || len(invalid) > 0 {
		if !*quiet {
			fmt.Fprintf(os.Stderr, "\nVerified %d files: %d valid, %d invalid, %d errors\n",
				len(allFiles), validCount, len(invalid), len(errors))
		}
		return 1
	}

	if !*quiet {
		fmt.Printf("All %d file(s) verified successfully\n", validCount)
	}
	return 0
}

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	style := fs.String("style", "", "Comment style (go|python|c|sql|html|shell|ruby|js)")
	fs.Parse(args)

	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "Error: no files specified\n")
		return 1
	}

	// Expand files
	allFiles, err := expandFiles(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}

	validCount := 0
	invalidCount := 0
	errorCount := 0

	for _, file := range allFiles {
		config := getConfig(file, *style)
		reader := hashfile.NewReader(config)

		valid, err := reader.VerifyFile(file)
		if err != nil {
			fmt.Printf("✗ %s (error: %v)\n", file, err)
			errorCount++
		} else if valid {
			fmt.Printf("✓ %s\n", file)
			validCount++
		} else {
			fmt.Printf("✗ %s (integrity check failed)\n", file)
			invalidCount++
		}
	}

	// Summary
	fmt.Printf("\nTotal: %d files, %d valid, %d invalid, %d errors\n",
		len(allFiles), validCount, invalidCount, errorCount)

	if invalidCount > 0 || errorCount > 0 {
		return 1
	}
	return 0
}

// getConfig returns configuration based on file extension or explicit style
func getConfig(filename, styleFlag string) hashfile.Config {
	if styleFlag != "" {
		return getConfigForStyle(styleFlag)
	}
	ext := filepath.Ext(filename)
	return hashfile.ConfigForExtension(ext)
}

// getConfigForStyle returns configuration for the specified style
func getConfigForStyle(style string) hashfile.Config {
	config := hashfile.DefaultConfig()

	switch style {
	case "go":
		config.CommentStyle = hashfile.GoStyle
	case "python", "py":
		config.CommentStyle = hashfile.PythonStyle
	case "c", "cpp", "java", "js", "javascript":
		config.CommentStyle = hashfile.CStyle
	case "sql":
		config.CommentStyle = hashfile.SQLStyle
	case "html", "xml":
		config.CommentStyle = hashfile.HTMLStyle
	case "shell", "sh", "bash":
		config.CommentStyle = hashfile.ShellStyle
	case "ruby", "rb":
		config.CommentStyle = hashfile.RubyStyle
	default:
		fmt.Fprintf(os.Stderr, "Warning: unknown style '%s', using default (Go)\n", style)
	}

	return config
}

// expandFiles expands file patterns and returns a list of files
func expandFiles(patterns []string) ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	for _, pattern := range patterns {
		// Check if it's a plain file (no wildcards)
		if !containsWildcard(pattern) {
			if !seen[pattern] {
				files = append(files, pattern)
				seen[pattern] = true
			}
			continue
		}

		// Expand glob pattern
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid pattern '%s': %v", pattern, err)
		}

		for _, match := range matches {
			// Only include regular files
			info, err := os.Stat(match)
			if err != nil {
				continue
			}
			if info.IsDir() {
				continue
			}

			if !seen[match] {
				files = append(files, match)
				seen[match] = true
			}
		}
	}

	return files, nil
}

// containsWildcard checks if a string contains glob wildcards
func containsWildcard(s string) bool {
	for _, c := range s {
		if c == '*' || c == '?' || c == '[' {
			return true
		}
	}
	return false
}
