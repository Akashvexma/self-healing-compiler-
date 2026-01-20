package main

import (
	"context"
	"time"
)

// ============================================================================
// CORE TYPES
// ============================================================================

// ErrorType classifies the nature of a compilation/execution error
type ErrorType int

const (
	ErrorTypeUnknown        ErrorType = iota
	ErrorTypeInfrastructure           // go.mod exists, missing dependencies, etc.
	ErrorTypeSyntax                   // Parse error, invalid syntax
	ErrorTypeType                     // Type mismatch, undefined symbols
	ErrorTypeLogic                    // Test failures, logic errors
	ErrorTypeRuntime                  // Crashes, segfaults, panics
	ErrorTypeSuccess                  // No error
)

func (e ErrorType) String() string {
	switch e {
	case ErrorTypeInfrastructure:
		return "infrastructure"
	case ErrorTypeSyntax:
		return "syntax"
	case ErrorTypeType:
		return "type"
	case ErrorTypeLogic:
		return "logic"
	case ErrorTypeRuntime:
		return "runtime"
	case ErrorTypeSuccess:
		return "success"
	default:
		return "unknown"
	}
}

// CompilationResult holds the output of a single compilation attempt
type CompilationResult struct {
	Success       bool
	ExitCode      int
	CompileErrors []string // Compilation-time errors
	TestErrors    []string // Test-time failures
	Output        string   // Raw combined output
	ErrorType     ErrorType
	ExecutionTime time.Duration
}

// ExecutionMetrics tracks performance and behavior across iterations
type ExecutionMetrics struct {
	IterationCount     int
	TotalTime          time.Duration
	PromptSizes        []int           // Prompt size per iteration
	LLMResponseTimes   []time.Duration // LLM latency per iteration
	LastErrorType      ErrorType
	SameErrorCount     int      // Consecutive identical errors
	ExtractedLanguages []string // Main, Test
}

// LLMContext maintains stateful conversation with the LLM
type LLMContext struct {
	ConversationTokens []int       // Ollama context for multi-turn conversation
	PromptHistory      []string    // Track all prompts sent to LLM
	ErrorHistory       []ErrorType // Track error types seen
	AttemptCount       int
	LastErrorMessage   string
}

// ExecutionJob represents a single user request being processed
type ExecutionJob struct {
	ID            string
	Language      string
	UserPrompt    string
	Model         string
	MaxIterations int
	Timeout       time.Duration

	Ctx       context.Context
	Cancel    context.CancelFunc
	Status    string // "pending", "running", "completed", "aborted"
	StartTime time.Time

	LLMCtx  LLMContext
	Metrics ExecutionMetrics

	FinalResult *CompilationResult
	AbortReason string
}

// ============================================================================
// CONFIGURATION & LIMITS
// ============================================================================

const (
	DefaultMaxIterations    = 10
	DefaultTimeout          = 5 * time.Minute
	DefaultLLMResponseTime  = 30 * time.Second
	DefaultCompileTimeout   = 30 * time.Second
	MaxPromptSize           = 50 * 1024 // 50KB
	SameErrorThreshold      = 3         // Abort if same error 3x
	MaxPromptSizeGrowthRate = 1.5       // Abort if prompt grows by 1.5x
)

// ============================================================================
// RESPONSE TYPES FOR WEBSOCKET
// ============================================================================

type WSMessageType string

const (
	WSTypeIteration  WSMessageType = "iteration"
	WSTypeCompletion WSMessageType = "completion"
	WSTypeAbort      WSMessageType = "abort"
	WSTypeError      WSMessageType = "error"
)

type WSIterationData struct {
	Iteration            int    `json:"iteration"`
	Status               string `json:"status"` // "generating", "compiling", "testing"
	MainCode             string `json:"mainCode"`
	TestCode             string `json:"testCode"`
	CompilerOutput       string `json:"compilerOutput"`
	CompiledSuccessfully bool   `json:"compiledSuccessfully"`
	ErrorType            string `json:"errorType"`
	ElapsedSeconds       int    `json:"elapsedSeconds"`
	PromptSize           int    `json:"promptSize"`
	LLMResponseTime      int    `json:"llmResponseTime"`
}

type WSCompletionData struct {
	FinalStatus     string `json:"finalStatus"` // "success" or "aborted"
	TotalIterations int    `json:"totalIterations"`
	TotalTime       string `json:"totalTime"`
	Code            string `json:"code"`
	Tests           string `json:"tests"`
}

type WSAbortData struct {
	Reason        string `json:"reason"` // "max_iterations", "same_error_3x", "timeout", "llm_error"
	Iteration     int    `json:"iteration"`
	LastError     string `json:"lastError"`
	LastErrorType string `json:"lastErrorType"`
}

type WSMessage struct {
	Type WSMessageType `json:"type"`
	Data interface{}   `json:"data"`
}

// ============================================================================
// HTTP API TYPES
// ============================================================================

type CompileRequest struct {
	Language      string `json:"language"` // "go", "python", "cpp"
	Prompt        string `json:"prompt"`
	Model         string `json:"model"`
	MaxIterations int    `json:"maxIterations"`
	Timeout       int    `json:"timeout"` // seconds
}

type CompileResponse struct {
	JobID  string      `json:"jobId"`
	Status string      `json:"status"`
	Data   interface{} `json:"data"`
}

type JobStatusResponse struct {
	JobID          string      `json:"jobId"`
	Status         string      `json:"status"`
	Iteration      int         `json:"iteration"`
	StartTime      int64       `json:"startTime"`
	ElapsedSeconds int         `json:"elapsedSeconds"`
	Data           interface{} `json:"data"`
}
