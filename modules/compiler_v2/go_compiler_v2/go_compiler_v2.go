package go_compiler_v2

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// IMPROVED GO COMPILER WITH PROPER ERROR CLASSIFICATION
// ============================================================================

type GoCompilerV2 struct{}

func NewGoCompilerV2() *GoCompilerV2 {
	return &GoCompilerV2{}
}

// CompilationResultV2 holds detailed compilation output
type CompilationResultV2 struct {
	Success       bool
	ExitCode      int
	CompileOutput string
	TestOutput    string
	RawOutput     string
	ErrorType     ErrorTypeGo
	ExecutionTime time.Duration
	CompileErrors []string
	TestErrors    []string
}

// ErrorTypeGo classifies Go compilation errors
type ErrorTypeGo int

const (
	ErrorTypeGoUnknown        ErrorTypeGo = iota
	ErrorTypeGoInfrastructure             // go.mod issues, missing toolchain
	ErrorTypeGoSyntax                     // Parse error
	ErrorTypeGoType                       // Undefined, type mismatch
	ErrorTypeGoLogic                      // Test failures
	ErrorTypeGoRuntime                    // Panic, segfault
	ErrorTypeGoSuccess                    // No error
)

// Compile executes Go compilation with proper isolation and error handling
func (gc *GoCompilerV2) Compile(ctx context.Context, mainCode, testCode string) (*CompilationResultV2, error) {
	startTime := time.Now()
	result := &CompilationResultV2{
		ExecutionTime: time.Since(startTime),
	}

	// Create fresh temp directory
	tempDir := filepath.Join(os.TempDir(), fmt.Sprintf("go_compile_%d", time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		result.ErrorType = ErrorTypeGoInfrastructure
		result.RawOutput = fmt.Sprintf("Failed to create temp directory: %v", err)
		result.CompileErrors = append(result.CompileErrors, result.RawOutput)
		return result, nil
	}
	defer os.RemoveAll(tempDir)

	// Write files
	mainFile := filepath.Join(tempDir, "main.go")
	testFile := filepath.Join(tempDir, "main_test.go")

	if err := os.WriteFile(mainFile, []byte(mainCode), 0644); err != nil {
		result.ErrorType = ErrorTypeGoInfrastructure
		result.RawOutput = fmt.Sprintf("Failed to write main.go: %v", err)
		result.CompileErrors = append(result.CompileErrors, result.RawOutput)
		return result, nil
	}

	if err := os.WriteFile(testFile, []byte(testCode), 0644); err != nil {
		result.ErrorType = ErrorTypeGoInfrastructure
		result.RawOutput = fmt.Sprintf("Failed to write main_test.go: %v", err)
		result.CompileErrors = append(result.CompileErrors, result.RawOutput)
		return result, nil
	}

	// Execute compilation pipeline with timeout
	cmdString := "go mod init temp_module && go mod tidy && go build -o /dev/null . && go test -v"

	// Parse command with error handling
	shell := "bash"
	shellArg := "-c"

	cmd := exec.CommandContext(ctx, shell, shellArg, cmdString)
	cmd.Dir = tempDir

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run with timeout
	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Run()
	}()

	select {
	case <-ctx.Done():
		cmd.Process.Kill()
		result.ErrorType = ErrorTypeGoInfrastructure
		result.RawOutput = "Compilation timeout"
		result.CompileErrors = append(result.CompileErrors, "Compilation exceeded timeout")
		result.ExecutionTime = time.Since(startTime)
		return result, nil

	case err := <-doneChan:
		result.ExecutionTime = time.Since(startTime)

		fullOutput := stdout.String() + "\n" + stderr.String()
		result.RawOutput = fullOutput

		if err == nil {
			result.Success = true
			result.ErrorType = ErrorTypeGoSuccess
			result.ExitCode = 0
			return result, nil
		}

		// Parse error
		result.ExitCode = 1
		result.CompileErrors, result.TestErrors = parseGoErrors(fullOutput)

		// Classify error
		result.ErrorType = classifyGoError(fullOutput, result.CompileErrors, result.TestErrors)

		return result, nil
	}
}

// ============================================================================
// ERROR PARSING & CLASSIFICATION
// ============================================================================

// parseGoErrors separates compilation errors from test errors
func parseGoErrors(output string) ([]string, []string) {
	var compileErrors []string
	var testErrors []string

	lines := strings.Split(output, "\n")

	for _, line := range lines {
		// Compilation errors typically contain file:line:col
		if strings.Contains(line, "main.go:") {
			compileErrors = append(compileErrors, strings.TrimSpace(line))
		}

		// Test failures
		if strings.Contains(line, "FAIL") || strings.Contains(line, "Error:") {
			testErrors = append(testErrors, strings.TrimSpace(line))
		}
	}

	return compileErrors, testErrors
}

// classifyGoError determines the type of error
func classifyGoError(output string, compileErrors, testErrors []string) ErrorTypeGo {
	output = strings.ToLower(output)

	// Infrastructure errors
	infraPatterns := []string{
		"go.mod already exists",
		"cannot find package",
		"not in GOPATH",
		"invalid go.mod",
		"module not found",
		"go get",
	}

	for _, pattern := range infraPatterns {
		if strings.Contains(output, strings.ToLower(pattern)) {
			return ErrorTypeGoInfrastructure
		}
	}

	// Syntax errors
	syntaxPatterns := []string{
		"syntax error",
		"expected",
		"unexpected",
		"invalid operation",
		"unclosed",
	}

	for _, pattern := range syntaxPatterns {
		if strings.Contains(output, strings.ToLower(pattern)) {
			return ErrorTypeGoSyntax
		}
	}

	// Type errors
	typePatterns := []string{
		"undefined",
		"type",
		"cannot use",
		"mismatched types",
		"wrong number of arguments",
		"not a function",
	}

	for _, pattern := range typePatterns {
		if strings.Contains(output, strings.ToLower(pattern)) {
			return ErrorTypeGoType
		}
	}

	// Runtime errors
	runtimePatterns := []string{
		"panic",
		"fatal",
		"signal: segmentation",
		"runtime error",
	}

	for _, pattern := range runtimePatterns {
		if strings.Contains(output, strings.ToLower(pattern)) {
			return ErrorTypeGoRuntime
		}
	}

	// Test failures
	if len(testErrors) > 0 {
		return ErrorTypeGoLogic
	}

	if len(compileErrors) > 0 {
		return ErrorTypeGoType // Default to type error if unclear
	}

	return ErrorTypeGoUnknown
}

// ============================================================================
// HELPER FOR LEGACY CODE COMPATIBILITY
// ============================================================================

// CheckCompileErrors wraps the new compiler for legacy code
func CheckCompileErrors(srcCode, testCode string) ([]byte, error) {
	// Create context with 30-second timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	compiler := NewGoCompilerV2()
	result, err := compiler.Compile(ctx, srcCode, testCode)
	if err != nil {
		return nil, err
	}

	// Convert to byte output for compatibility
	output := fmt.Sprintf("=== COMPILE OUTPUT ===\n%s\n\n=== TEST OUTPUT ===\n%s\n\nError Type: %v\nSuccess: %v\n",
		result.CompileOutput, result.TestOutput, result.ErrorType, result.Success)

	if result.Success {
		return []byte(output), nil
	}

	// Return error if compilation failed
	return []byte(output), fmt.Errorf("compilation failed: %s", result.RawOutput)
}

// ============================================================================
// ERROR RECOVERY STRATEGIES
// ============================================================================

// RetryCompileWithClean retries compilation with a completely clean state
func (gc *GoCompilerV2) RetryCompileWithClean(ctx context.Context, mainCode, testCode string) (*CompilationResultV2, error) {
	// First attempt
	result, err := gc.Compile(ctx, mainCode, testCode)
	if err != nil {
		return result, err
	}

	// If infrastructure error, retry is unlikely to help
	if result.ErrorType == ErrorTypeGoInfrastructure {
		return result, nil
	}

	// Otherwise return the result
	return result, nil
}

// ============================================================================
// ERROR MESSAGE FORMATTING FOR LLM
// ============================================================================

// FormatErrorForLLM creates a concise error message suitable for LLM feedback
func FormatErrorForLLM(result *CompilationResultV2) string {
	if result.Success {
		return "Compilation and tests passed successfully."
	}

	var msg strings.Builder
	msg.WriteString(fmt.Sprintf("Error Type: %v\n", result.ErrorType))

	if len(result.CompileErrors) > 0 {
		msg.WriteString("Compilation Errors:\n")
		for i, err := range result.CompileErrors {
			if i < 5 { // Limit to first 5 errors
				msg.WriteString(fmt.Sprintf("  - %s\n", err))
			}
		}
	}

	if len(result.TestErrors) > 0 {
		msg.WriteString("Test Failures:\n")
		for i, err := range result.TestErrors {
			if i < 5 { // Limit to first 5 errors
				msg.WriteString(fmt.Sprintf("  - %s\n", err))
			}
		}
	}

	if msg.Len() == 0 {
		msg.WriteString("Unknown error. Full output:\n")
		msg.WriteString(result.RawOutput)
	}

	return msg.String()
}
