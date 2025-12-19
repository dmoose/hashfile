# hashfile

[![Go Reference](https://pkg.go.dev/badge/github.com/dmoose/hashfile.svg)](https://pkg.go.dev/github.com/dmoose/hashfile)
[![Go Report Card](https://goreportcard.com/badge/github.com/dmoose/hashfile)](https://goreportcard.com/report/github.com/dmoose/hashfile)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A lightweight, efficient tool for adding CRC32-based integrity comments to source code files. `hashfile` helps you detect unintended changes to your source files by embedding a checksum comment that can be quickly verified.

## Features

- **Efficient streaming algorithm** - Single buffer allocation with sliding window, processes files of any size
- **No-op on unchanged files** - Only modifies files when the integrity comment is missing or incorrect
- **Multiple language support** - Auto-detects comment style based on file extension (Go, Python, C/C++, SQL, HTML, Shell, Ruby, JavaScript, CSS, Templ)
- **Preserves file attributes** - Maintains permissions and ownership when updating files
- **Line ending aware** - Preserves CRLF vs LF line endings
- **Zero dependencies** - Uses only Go standard library

## Use Cases

- Verify source code hasn't been modified during deployment
- Detect accidental edits to configuration files
- Ensure generated code remains unchanged
- Quick integrity checks for code reviews
- Build system verification step

**Note:** This is NOT a security tool. It uses CRC32 for fast integrity checking, not cryptographic security. Do not use this to detect malicious modifications.

## Installation

### From Source

```bash
go install github.com/dmoose/hashfile/cmd/hashfile@latest
```

### Build from Repository

```bash
git clone https://github.com/dmoose/hashfile.git
cd hashfile
make build
# Binary will be in bin/hashfile
```

### As a Library

```bash
go get github.com/dmoose/hashfile
```

## Command Line Usage

### Add/Update Integrity Comments

Add integrity comments to one or more files:

```bash
# Single file
hashfile add main.go

# Multiple files
hashfile add main.go handler.go utils.go

# Using glob patterns
hashfile add src/*.go

# Specify comment style explicitly
hashfile add -style=python script.txt
```

**What happens:**
- Calculates CRC32 of file content (excluding the integrity comment itself)
- Adds a comment line at the end: `// FileIntegrity: ABCD1234`
- If comment already exists and is correct, file is not modified (no-op)
- If comment exists but is wrong, it's updated with the correct hash

### Verify File Integrity

Check if files have been modified:

```bash
# Verify files (silent mode, use exit code)
hashfile verify *.go
echo $?  # 0 = all valid, 1 = invalid or error

# Quiet mode (no output at all)
hashfile verify -q *.go
```

**Exit codes:**
- `0` - All files verified successfully
- `1` - One or more files invalid or errors occurred

### Check with Output

Display human-readable verification status:

```bash
hashfile check src/*.go
```

**Example output:**
```
✓ src/main.go
✓ src/handler.go
✗ src/utils.go (integrity check failed)

Total: 3 files, 2 valid, 1 invalid, 0 errors
```

## Library Usage

### Basic Example

```go
package main

import (
    "fmt"
    "log"
    
    "github.com/dmoose/hashfile"
)

func main() {
    // Add integrity comment (auto-detects .go extension)
    if err := hashfile.ProcessFile("main.go"); err != nil {
        log.Fatal(err)
    }
    
    // Verify integrity
    valid, err := hashfile.VerifyFile("main.go")
    if err != nil {
        log.Fatal(err)
    }
    
    if valid {
        fmt.Println("File integrity verified!")
    } else {
        fmt.Println("File has been modified!")
    }
}
```

### Custom Configuration

```go
package main

import (
    "github.com/dmoose/hashfile"
)

func main() {
    // Create custom configuration
    config := hashfile.Config{
        CommentStyle: hashfile.PythonStyle,
        BufferSize:   128 * 1024, // 128KB buffer
    }
    
    // Use custom config
    writer := hashfile.NewWriter(config)
    writer.ProcessFile("script.py")
    
    reader := hashfile.NewReader(config)
    valid, _ := reader.VerifyFile("script.py")
}
```

### Supported Comment Styles

```txt
hashfile.GoStyle      // FileIntegrity: ABCD1234
hashfile.PythonStyle  # FileIntegrity: ABCD1234
hashfile.CStyle       // FileIntegrity: ABCD1234
hashfile.SQLStyle     -- FileIntegrity: ABCD1234
hashfile.HTMLStyle    <!-- FileIntegrity: ABCD1234 -->
hashfile.ShellStyle   # FileIntegrity: ABCD1234
hashfile.RubyStyle    # FileIntegrity: ABCD1234
hashfile.JSStyle      // FileIntegrity: ABCD1234
hashfile.CSSStyle     /* FileIntegrity: ABCD1234 */
hashfile.TemplStyle   const FileIntegrity = "ABCD1234"
```

**Note:** `TemplStyle` uses a Go constant declaration instead of a comment. Since [templ](https://templ.guide/) files compile to Go code, this allows the integrity hash to be embedded in generated HTML comments for traceability (e.g., `<!-- Template Integrity: { FileIntegrity } -->`).

### Auto-Detection by Extension

The library automatically selects the appropriate comment style:

| Extension | Comment Style |
|-----------|---------------|
| `.go` | `// ...` |
| `.py` | `# ...` |
| `.c`, `.h`, `.cpp`, `.java`, `.js`, `.ts` | `// ...` |
| `.sql` | `-- ...` |
| `.html`, `.xml` | `<!-- ... -->` |
| `.sh`, `.bash` | `# ...` |
| `.rb` | `# ...` |
| `.css`, `.scss`, `.sass` | `/* ... */` |
| `.templ` | `const FileIntegrity = "..."` |

## How It Works

### Algorithm Overview

1. **Streaming with Sliding Window**
   - Reads file in chunks (default 64KB buffer)
   - Maintains a small sliding window (≈30 bytes) at the end
   - Calculates CRC32 of all content except the window
   - Single buffer allocation for efficiency

2. **At End of File**
   - Examines the window for existing integrity comment
   - If found and correct: writes temp file, compares to original, no-op if identical
   - If found but wrong: updates comment with new CRC
   - If not found: adds new comment

3. **Content Hashing**
   - CRC32 includes all file content EXCEPT the integrity comment line itself
   - All content including trailing newlines gets CRC'd
   - If a file doesn't end with a newline, one is added and CRC'd before adding the comment

4. **No-Op Optimization**
   - Always writes to temp file during streaming
   - If existing integrity comment has matching CRC, deletes temp and leaves original untouched
   - When file is modified, preserves attributes (permissions, ownership) during replacement

### Example

**Before:**
```go
package main

func main() {
    println("Hello")
}
```

**After adding integrity:**
```go
package main

func main() {
    println("Hello")
}

// FileIntegrity: A1B2C3D4
```

**Example with CSS:**
```css
body {
    color: red;
}

/* FileIntegrity: E5F6A7B8 */
```

**Example with Templ:**
```templ
package components

templ Hello() {
    <div>Hello</div>
}

const FileIntegrity = "C9D0E1F2"
```

**If you modify the code:**
```go
package main

func main() {
    println("Hello, World!")  // Changed
}
// FileIntegrity: A1B2C3D4  // Now invalid!
```

Verification will fail until you run `hashfile add` again to update the hash.

## Performance

The streaming algorithm is optimized for efficiency:

- **Memory:** Single 64KB buffer allocation (configurable)
- **I/O:** Buffered reads/writes, minimal system calls
- **Processing:** Each byte processed exactly once for CRC
- **Allocations:** Minimal heap allocations (30 for process, 7 for verify on ~26KB file)

Benchmark results (Apple M1 Max):
```
BenchmarkProcessFile-10    	    3615	    304277 ns/op	   71972 B/op	      30 allocs/op
BenchmarkVerifyFile-10     	   66835	     17837 ns/op	   66386 B/op	       7 allocs/op
```

On a ~26KB test file: ~3,600 process operations/sec, ~66,000 verify operations/sec.

## Testing

Run the test suite:

```bash
# All tests
make test

# With coverage report
make test-coverage

# Benchmarks
make bench
```

## Building

```bash
# Build binary
make build

# Build and test
make all

# Install to $GOPATH/bin
make install

# Clean artifacts
make clean
```

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass: `make test`
5. Run linter: `make lint` (requires golangci-lint)
6. Submit a pull request

## License

MIT License - see [LICENSE](LICENSE) file for details.

## FAQ

**Q: Why CRC32 instead of SHA256 or other cryptographic hash?**  
A: This tool is designed for detecting *accidental* changes, not malicious tampering. CRC32 is extremely fast and sufficient for this purpose. If you need cryptographic security, use proper code signing tools.

**Q: What if I need to edit a file with an integrity comment?**  
A: Just edit it normally. The integrity comment will show the file has changed. Run `hashfile add` again to update the comment with the new hash.

**Q: Does this work with binary files?**  
A: Technically yes, but don't. Will append text to binary file which is almost certainly not what you wanted.

**Q: Can I use this in CI/CD pipelines?**  
A: Yes! Use `hashfile verify -q *.go` and check the exit code. It's silent and fast.

**Q: What happens if I manually change the hash in the comment?**  
A: Verification will fail. The hash must match the actual CRC32 of the file content.

**Q: Does it modify the file's timestamp?**  
A: Only if the file actually needs to be updated. If the hash is already correct (no-op case), the file is not touched and timestamps are preserved.
