package extraction

import (
	"fmt"
	"regexp"
	"strings"
)

// ============================================================================
// EXTRACTION STRATEGIES
// ============================================================================

// ExtractionStrategy defines an interface for parsing code from LLM responses
type ExtractionStrategy interface {
	Extract(response string) (main, test string, err error)
	Name() string
}

// ============================================================================
// STRATEGY 1: Standard Format (```code ... ```)
// ============================================================================

type StandardFormatStrategy struct{}

func (s StandardFormatStrategy) Name() string {
	return "standard_format"
}

func (s StandardFormatStrategy) Extract(response string) (main, test string, err error) {
	// Match all code blocks: ```[language]\ncode\n```
	codeBlockPattern := regexp.MustCompile("(?s)```(?:[a-z]+)?\\n(.*?)```")
	matches := codeBlockPattern.FindAllStringSubmatch(response, -1)

	if len(matches) < 2 {
		return "", "", fmt.Errorf("%s: expected at least 2 code blocks, found %d", s.Name(), len(matches))
	}

	mainCode := strings.TrimSpace(matches[0][1])
	testCode := strings.TrimSpace(matches[1][1])

	if len(mainCode) == 0 || len(testCode) == 0 {
		return "", "", fmt.Errorf("%s: empty code blocks", s.Name())
	}

	return mainCode, testCode, nil
}

// ============================================================================
// STRATEGY 2: Package/Function Declaration Split
// ============================================================================

type PackageMainStrategy struct{}

func (s PackageMainStrategy) Name() string {
	return "package_main_split"
}

func (s PackageMainStrategy) Extract(response string) (main, test string, err error) {
	// Split by "package main" occurrences
	parts := strings.Split(response, "package main")

	if len(parts) < 3 {
		return "", "", fmt.Errorf("%s: expected at least 2 'package main' declarations, found %d", s.Name(), len(parts)-1)
	}

	// Reconstruct: "package main" + content
	mainCode := strings.TrimSpace("package main" + parts[1])
	testCode := strings.TrimSpace("package main" + parts[2])

	// Remove markdown backticks if present
	mainCode = strings.Trim(mainCode, "`")
	testCode = strings.Trim(testCode, "`")

	mainCode = strings.TrimSpace(mainCode)
	testCode = strings.TrimSpace(testCode)

	if len(mainCode) < 50 || len(testCode) < 50 {
		return "", "", fmt.Errorf("%s: code blocks too small", s.Name())
	}

	return mainCode, testCode, nil
}

// ============================================================================
// STRATEGY 3: Test Function Marker Split
// ============================================================================

type TestMarkerStrategy struct{}

func (s TestMarkerStrategy) Name() string {
	return "test_marker_split"
}

func (s TestMarkerStrategy) Extract(response string) (main, test string, err error) {
	// Look for "func Test" pattern - everything before is main, from Test onwards is test
	testMarkerPattern := regexp.MustCompile(`(?m)^func\s+Test`)
	loc := testMarkerPattern.FindStringIndex(response)

	if loc == nil {
		return "", "", fmt.Errorf("%s: no 'func Test' marker found", s.Name())
	}

	mainCode := strings.TrimSpace(response[:loc[0]])
	testCode := strings.TrimSpace(response[loc[0]:])

	// Prepend package main if needed
	if !strings.HasPrefix(mainCode, "package") {
		mainCode = "package main\n" + mainCode
	}
	if !strings.HasPrefix(testCode, "package") {
		testCode = "package main\nimport \"testing\"\n" + testCode
	}

	mainCode = strings.Trim(mainCode, "`")
	testCode = strings.Trim(testCode, "`")

	if len(mainCode) < 30 || len(testCode) < 30 {
		return "", "", fmt.Errorf("%s: code blocks too small", s.Name())
	}

	return mainCode, testCode, nil
}

// ============================================================================
// STRATEGY 4: Code Block With Language Tags
// ============================================================================

type LanguageTagStrategy struct{}

func (s LanguageTagStrategy) Name() string {
	return "language_tag_split"
}

func (s LanguageTagStrategy) Extract(response string) (main, test string, err error) {
	// Match: ```go\ncode\n``` and capture multiple blocks
	goBlockPattern := regexp.MustCompile("(?s)```go\\n(.*?)```")
	matches := goBlockPattern.FindAllStringSubmatch(response, -1)

	if len(matches) < 2 {
		return "", "", fmt.Errorf("%s: expected at least 2 'go' code blocks, found %d", s.Name(), len(matches))
	}

	mainCode := strings.TrimSpace(matches[0][1])
	testCode := strings.TrimSpace(matches[1][1])

	if len(mainCode) == 0 || len(testCode) == 0 {
		return "", "", fmt.Errorf("%s: empty code blocks", s.Name())
	}

	return mainCode, testCode, nil
}

// ============================================================================
// STRATEGY 5: Single File Fallback (return all as main, empty test)
// ============================================================================

type SingleFileStrategy struct{}

func (s SingleFileStrategy) Name() string {
	return "single_file_fallback"
}

func (s SingleFileStrategy) Extract(response string) (main, test string, err error) {
	// Extract any code block
	codeBlockPattern := regexp.MustCompile("(?s)```(?:[a-z]+)?\\n(.*?)```")
	matches := codeBlockPattern.FindAllStringSubmatch(response, -1)

	if len(matches) == 0 {
		// No code block found, try raw code
		if len(response) > 50 {
			return strings.TrimSpace(response), "", nil
		}
		return "", "", fmt.Errorf("%s: no code found", s.Name())
	}

	// Use first block as everything
	code := strings.TrimSpace(matches[0][1])

	// Simple heuristic: if contains "func Test", split there
	if strings.Contains(code, "func Test") {
		idx := strings.Index(code, "func Test")
		main := strings.TrimSpace(code[:idx])
		test := strings.TrimSpace(code[idx:])

		if !strings.HasPrefix(test, "package") {
			test = "package main\nimport \"testing\"\n" + test
		}

		return main, test, nil
	}

	// Otherwise return first block as main, generate minimal test
	return code, "", nil
}

// ============================================================================
// EXTRACTOR: Try strategies in sequence
// ============================================================================

type Extractor struct {
	strategies []ExtractionStrategy
}

func NewExtractor() *Extractor {
	return &Extractor{
		strategies: []ExtractionStrategy{
			StandardFormatStrategy{},
			LanguageTagStrategy{},
			TestMarkerStrategy{},
			PackageMainStrategy{},
			SingleFileStrategy{},
		},
	}
}

// Extract tries all strategies and returns the first successful result
func (e *Extractor) Extract(response string) (main, test string, strategy string, err error) {
	var lastErr error

	for _, strat := range e.strategies {
		main, test, err := strat.Extract(response)
		if err == nil {
			return main, test, strat.Name(), nil
		}
		lastErr = err
	}

	return "", "", "", fmt.Errorf("all extraction strategies failed; last error: %v", lastErr)
}

// ExtractWithFallback tries extraction, and if all fail, returns the raw response with minimal parsing
func (e *Extractor) ExtractWithFallback(response string) (main, test string, strategy string) {
	main, test, strategy, err := e.Extract(response)
	if err == nil {
		return main, test, strategy
	}

	// Fallback: try to extract any ```code``` block
	codeBlockPattern := regexp.MustCompile("(?s)```(?:[a-z]+)?\\n(.*?)```")
	matches := codeBlockPattern.FindAllStringSubmatch(response, -1)

	if len(matches) > 0 {
		main = strings.TrimSpace(matches[0][1])
		if len(matches) > 1 {
			test = strings.TrimSpace(matches[1][1])
		}
		return main, test, "emergency_fallback"
	}

	// Absolute fallback: return raw response
	return strings.TrimSpace(response), "", "raw_response"
}
