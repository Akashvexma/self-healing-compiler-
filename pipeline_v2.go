package main

import (
	"context"
	"fmt"
	"llama/modules/extraction"
	ollamaimplementation "llama/modules/ollama-implementation"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================================
// PRODUCTION COMPILER PIPELINE - V2
// ============================================================================

// RunCompilationJob executes the production compiler pipeline with safeguards
func RunCompilationJob(job *ExecutionJob, conn *websocket.Conn) {
	defer func() {
		job.Status = "completed"
		if r := recover(); r != nil {
			fmt.Printf("[PANIC] Job %s: %v\n", job.ID, r)
			job.AbortReason = "internal_panic"
		}
	}()

	job.Status = "running"
	job.StartTime = time.Now()

	extractor := extraction.NewExtractor()

	// ============================================================================
	// MAIN ITERATION LOOP WITH SAFEGUARDS
	// ============================================================================

	for iteration := 1; iteration <= job.MaxIterations; iteration++ {
		select {
		case <-job.Ctx.Done():
			// User cancelled
			job.Status = "aborted"
			job.AbortReason = "user_cancelled"
			sendAbortMessage(conn, job, "User cancelled execution")
			return

		default:
		}

		// Check total timeout
		if time.Since(job.StartTime) > job.Timeout {
			job.Status = "aborted"
			job.AbortReason = "total_timeout"
			sendAbortMessage(conn, job, "Total timeout exceeded")
			return
		}

		job.Metrics.IterationCount = iteration

		fmt.Printf("[Job %s] Iteration %d/%d started\n", job.ID, iteration, job.MaxIterations)

		// ======================================================================
		// PHASE 1: GENERATE CODE WITH LLM
		// ======================================================================

		fmt.Printf("[Job %s] Phase 1: Generating code...\n", job.ID)

		prompt, promptSize := buildPrompt(job, iteration)

		// Check prompt size growth
		if promptSize > int(MaxPromptSize) {
			job.Status = "aborted"
			job.AbortReason = "prompt_size_exceeded"
			sendAbortMessage(conn, job, fmt.Sprintf("Prompt size exceeded: %d > %d bytes", promptSize, MaxPromptSize))
			return
		}

		job.Metrics.PromptSizes = append(job.Metrics.PromptSizes, promptSize)

		// LLM Call with timeout
		llmStart := time.Now()
		// Note: GetOllamaResponse doesn't currently support context timeout
		// TODO: Add context support to ollama module
		llmResponse, updatedContext, llmErr := ollamaimplementation.GetOllamaResponse(prompt, job.LLMCtx.ConversationTokens, job.Model)
		llmTime := time.Since(llmStart)

		job.Metrics.LLMResponseTimes = append(job.Metrics.LLMResponseTimes, llmTime)

		if llmErr != nil {
			fmt.Printf("[Job %s] LLM error: %v\n", job.ID, llmErr)
			job.Status = "aborted"
			job.AbortReason = "llm_error"
			sendAbortMessage(conn, job, fmt.Sprintf("LLM error: %v", llmErr))
			return
		}

		if llmResponse == "" {
			fmt.Printf("[Job %s] Empty LLM response\n", job.ID)
			job.Status = "aborted"
			job.AbortReason = "empty_llm_response"
			sendAbortMessage(conn, job, "LLM returned empty response")
			return
		}

		job.LLMCtx.ConversationTokens = updatedContext
		job.LLMCtx.PromptHistory = append(job.LLMCtx.PromptHistory, prompt)

		// ======================================================================
		// PHASE 2: EXTRACT CODE FROM LLM RESPONSE
		// ======================================================================

		fmt.Printf("[Job %s] Phase 2: Extracting code...\n", job.ID)

		mainCode, testCode, extractionStrategy := extractor.ExtractWithFallback(llmResponse)

		if mainCode == "" {
			fmt.Printf("[Job %s] Extraction failed, no code recovered\n", job.ID)
			job.Status = "aborted"
			job.AbortReason = "extraction_failed"
			sendAbortMessage(conn, job, "Failed to extract code from LLM response")
			return
		}

		fmt.Printf("[Job %s] Extraction strategy: %s\n", job.ID, extractionStrategy)

		// ======================================================================
		// PHASE 3: COMPILE AND TEST
		// ======================================================================

		fmt.Printf("[Job %s] Phase 3: Compiling and testing...\n", job.ID)

		compileCtx, cancel := context.WithTimeout(job.Ctx, DefaultCompileTimeout)
		result, compileErr := compileLanguage(compileCtx, job.Language, mainCode, testCode)
		cancel()

		if compileErr != nil {
			fmt.Printf("[Job %s] Compilation error: %v\n", job.ID, compileErr)
			job.Status = "aborted"
			job.AbortReason = "compilation_failed"
			sendAbortMessage(conn, job, fmt.Sprintf("Compilation failed: %v", compileErr))
			return
		}

		// ======================================================================
		// PHASE 4: ANALYZE RESULTS
		// ======================================================================

		fmt.Printf("[Job %s] Phase 4: Analyzing results (success=%v, errorType=%s)\n", job.ID, result.Success, result.ErrorType.String())

		// Send iteration result
		sendIterationMessage(conn, job, iteration, mainCode, testCode, result)

		// Check if successful
		if result.Success {
			fmt.Printf("[Job %s] ✓ SUCCESS on iteration %d\n", job.ID, iteration)
			job.FinalResult = result
			job.Status = "completed"
			sendCompletionMessage(conn, job, mainCode, testCode)
			return
		}

		// ======================================================================
		// PHASE 5: ERROR ANALYSIS & LOOP CONTINUATION
		// ======================================================================

		fmt.Printf("[Job %s] Phase 5: Analyzing error (type=%s)...\n", job.ID, result.ErrorType.String())

		// Update error tracking
		job.LLMCtx.ErrorHistory = append(job.LLMCtx.ErrorHistory, result.ErrorType)
		job.LLMCtx.LastErrorMessage = strings.Join(result.CompileErrors, "; ")
		job.Metrics.LastErrorType = result.ErrorType

		// Check for infrastructure errors (don't feed to LLM)
		if result.ErrorType == ErrorTypeInfrastructure {
			fmt.Printf("[Job %s] Infrastructure error detected, retrying without LLM\n", job.ID)
			// TODO: Implement infrastructure error recovery (e.g., clean and retry)
			// For now, we can try once more
			if iteration < 2 {
				continue // Retry loop
			}
			job.Status = "aborted"
			job.AbortReason = "infrastructure_error_persistent"
			sendAbortMessage(conn, job, "Persistent infrastructure error")
			return
		}

		// Check for same error repeating (LLM stuck)
		if len(job.LLMCtx.ErrorHistory) >= SameErrorThreshold {
			lastErrors := job.LLMCtx.ErrorHistory[len(job.LLMCtx.ErrorHistory)-SameErrorThreshold:]
			allSame := true
			for _, e := range lastErrors {
				if e != result.ErrorType {
					allSame = false
					break
				}
			}

			if allSame && result.ErrorType != ErrorTypeSuccess {
				fmt.Printf("[Job %s] Same error %d times, LLM stuck\n", job.ID, SameErrorThreshold)
				job.Status = "aborted"
				job.AbortReason = "same_error_threshold"
				job.Metrics.SameErrorCount = SameErrorThreshold
				sendAbortMessage(conn, job, fmt.Sprintf("LLM stuck with same error after %d attempts", SameErrorThreshold))
				return
			}
		}

		// Check iteration limit
		if iteration >= job.MaxIterations {
			fmt.Printf("[Job %s] Max iterations reached (%d)\n", job.ID, job.MaxIterations)
			job.Status = "aborted"
			job.AbortReason = "max_iterations_reached"
			job.FinalResult = result
			sendAbortMessage(conn, job, fmt.Sprintf("Max iterations (%d) reached", job.MaxIterations))
			return
		}

		fmt.Printf("[Job %s] Continuing to iteration %d\n", job.ID, iteration+1)
	}

	// Should not reach here
	job.Status = "completed"
	job.AbortReason = "unknown"
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// buildPrompt constructs the prompt for the LLM, including error feedback
func buildPrompt(job *ExecutionJob, iteration int) (string, int) {
	var prompt strings.Builder

	// Initial prompt
	prompt.WriteString(job.UserPrompt)

	// Add language-specific format instructions
	switch job.Language {
	case "go":
		prompt.WriteString(goFormatInstructions)
	case "python":
		prompt.WriteString(pythonFormatInstructions)
	case "cpp":
		prompt.WriteString(cppFormatInstructions)
	}

	// Add error feedback if not first iteration
	if iteration > 1 && len(job.LLMCtx.ErrorHistory) > 0 {
		lastError := job.Metrics.LastErrorType
		lastMsg := job.LLMCtx.LastErrorMessage

		prompt.WriteString("\n\n=== ERROR FEEDBACK FROM ITERATION ")
		prompt.WriteString(fmt.Sprintf("%d", iteration-1))
		prompt.WriteString(" ===\n")
		prompt.WriteString(fmt.Sprintf("Error Type: %s\n", lastError.String()))
		prompt.WriteString(fmt.Sprintf("Error Message: %s\n", truncate(lastMsg, 500)))
		prompt.WriteString("Please fix the error and regenerate the code.\n")
	}

	promptSize := len(prompt.String())
	return prompt.String(), promptSize
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// compileLanguage delegates to the appropriate language compiler
func compileLanguage(ctx context.Context, language, mainCode, testCode string) (*CompilationResult, error) {
	switch language {
	case "go":
		return compileGo(ctx, mainCode, testCode)
	case "python":
		return compilePython(ctx, mainCode, testCode)
	case "cpp":
		return compileCPP(ctx, mainCode, testCode)
	default:
		return nil, fmt.Errorf("unsupported language: %s", language)
	}
}

// ============================================================================
// LANGUAGE-SPECIFIC COMPILERS (stubs - to be implemented)
// ============================================================================

func compileGo(ctx context.Context, mainCode, testCode string) (*CompilationResult, error) {
	// TODO: Implement Go compilation with proper error classification
	// This should use modules/compiler_v2/go_compiler_v2 with improvements:
	// - Fresh temp directory each iteration
	// - Timeout enforcement
	// - Proper error parsing
	return nil, fmt.Errorf("not yet implemented")
}

func compilePython(ctx context.Context, mainCode, testCode string) (*CompilationResult, error) {
	// TODO: Implement Python compilation
	return nil, fmt.Errorf("not yet implemented")
}

func compileCPP(ctx context.Context, mainCode, testCode string) (*CompilationResult, error) {
	// TODO: Implement C++ compilation
	return nil, fmt.Errorf("not yet implemented")
}

// ============================================================================
// WEBSOCKET MESSAGE SENDERS
// ============================================================================

func sendIterationMessage(conn *websocket.Conn, job *ExecutionJob, iteration int, mainCode, testCode string, result *CompilationResult) {
	data := WSIterationData{
		Iteration:            iteration,
		Status:               "compiled",
		MainCode:             mainCode,
		TestCode:             testCode,
		CompilerOutput:       result.Output,
		CompiledSuccessfully: result.Success,
		ErrorType:            result.ErrorType.String(),
		ElapsedSeconds:       int(time.Since(job.StartTime).Seconds()),
		PromptSize:           job.Metrics.PromptSizes[len(job.Metrics.PromptSizes)-1],
		LLMResponseTime:      int(job.Metrics.LLMResponseTimes[len(job.Metrics.LLMResponseTimes)-1].Milliseconds()),
	}

	msg := WSMessage{
		Type: WSTypeIteration,
		Data: data,
	}

	conn.WriteJSON(msg)
}

func sendCompletionMessage(conn *websocket.Conn, job *ExecutionJob, mainCode, testCode string) {
	data := WSCompletionData{
		FinalStatus:     "success",
		TotalIterations: job.Metrics.IterationCount,
		TotalTime:       time.Since(job.StartTime).String(),
		Code:            mainCode,
		Tests:           testCode,
	}

	msg := WSMessage{
		Type: WSTypeCompletion,
		Data: data,
	}

	conn.WriteJSON(msg)
}

func sendAbortMessage(conn *websocket.Conn, job *ExecutionJob, reason string) {
	lastErrorType := ErrorTypeUnknown.String()
	if len(job.LLMCtx.ErrorHistory) > 0 {
		lastErrorType = job.LLMCtx.ErrorHistory[len(job.LLMCtx.ErrorHistory)-1].String()
	}

	data := WSAbortData{
		Reason:        job.AbortReason,
		Iteration:     job.Metrics.IterationCount,
		LastError:     reason,
		LastErrorType: lastErrorType,
	}

	msg := WSMessage{
		Type: WSTypeAbort,
		Data: data,
	}

	conn.WriteJSON(msg)
}

// ============================================================================
// LANGUAGE-SPECIFIC FORMAT INSTRUCTIONS
// ============================================================================

const goFormatInstructions = `
IMPORTANT: Generate Go code in this exact format:
- Two code blocks separated by a blank line
- First block: package main with main() function and helper functions
- Second block: package main with import "testing" and Test* functions
- Use ONLY "package main" in both blocks
- Provide code only, no explanations
`

const pythonFormatInstructions = `
IMPORTANT: Generate Python code in this exact format:
- Two code blocks separated by a blank line
- First block: main code with functions and main() execution
- Second block: import unittest or pytest and test functions
- Provide code only, no explanations
`

const cppFormatInstructions = `
IMPORTANT: Generate C++ code in this exact format:
- Two code blocks separated by a blank line
- First block: main.cpp with function implementations and main()
- Second block: test functions using assert or gtest
- Include necessary #include statements
- Provide code only, no explanations
`
