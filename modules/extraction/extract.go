package extraction

import (
	"fmt"
	"regexp"
	"strings"
)

// var GoPrompt = "The code should be in the Go programming language. There should also be 3 robust test cases within the same file, these test cases should use 'testing' module. There should also be a main function inside of which the execution of the implemented function takes place. Please always provide the source code and no further explanation, The format should be ```go <yourcode + testcases> ```"

var GoPrompt = "\n\n=== FORMAT INSTRUCTIONS ===\nProvide code in ONE code block with TWO 'package main' declarations:\n```go\npackage main\n// main code with functions here\n\npackage main\nimport \"testing\"\n// test functions starting with Test here\n```\nMust have: 1) main() function or helper functions, 2) Test functions with *testing.T parameter"

var RustPrompt = "The code should be in the Rust programming language. There should also be 3 robust test cases within the same code. There should also be a main function inside of which all the execution takes place. Please only provide the source code and no further explanation, The format should be ```rust <yourcode + testcases> ```"

// func Extract(output string) string {
// 	parts := strings.Split(output, "```")
// 	var extracted = ""
// 	if strings.Contains(parts[1], "rust") {
// 		extracted = strings.TrimLeft(parts[1], "rust")
// 	} else {
// 		extracted = strings.TrimLeft(parts[1], "go")
// 	}
// 	return extracted
// }

// Extract extracts the code snippet between ``` blocks and separates main from test code.

func Extract(input string) (string, string, error) {
	// Try to find standard format with two separate code blocks
	codeBlockPattern := regexp.MustCompile("(?s)```(?:go)?\\n(.*?)```")
	matches := codeBlockPattern.FindAllStringSubmatch(input, -1)

	if len(matches) >= 2 {
		// We have at least 2 blocks
		mainCode := strings.TrimSpace(matches[0][1])
		testCode := strings.TrimSpace(matches[1][1])

		if len(mainCode) > 0 && len(testCode) > 0 {
			return mainCode, testCode, nil
		}
	}

	// If not found, try to split by "package main" occurrences
	// This handles LLM responses that don't use proper code blocks
	parts := strings.Split(input, "package main")
	if len(parts) >= 3 {
		// parts[0] is before first "package main", parts[1] is main code, parts[2] is test code
		mainCode := strings.TrimSpace("package main" + parts[1])
		testCode := strings.TrimSpace("package main" + parts[2])

		// Extract just the first function/main for mainCode
		mainLines := strings.Split(mainCode, "\n")
		testLines := strings.Split(testCode, "\n")

		mainCode = strings.Join(mainLines[:], "\n")
		testCode = strings.Join(testLines, "\n")

		if len(mainCode) > 50 && len(testCode) > 50 { // Sanity check
			return mainCode, testCode, nil
		}
	}

	return "", "", fmt.Errorf("improper LLM response: cannot extract code blocks")
}
