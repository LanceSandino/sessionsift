// Package transcript extracts individual tool invocations from Codex's JavaScript orchestration wrapper.
// @module NestedTools preserves each nested call's own arguments without evaluating wrapper code.
package transcript

import (
	"regexp"
	"strings"
)

type nestedToolCall struct {
	Name      string
	Arguments string
}

var nestedToolPattern = regexp.MustCompile(`\btools\.([A-Za-z][A-Za-z0-9_]*)\s*\(`)

func nestedToolCalls(input string) []nestedToolCall {
	matches := nestedToolPattern.FindAllStringSubmatchIndex(input, -1)
	calls := make([]nestedToolCall, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		end, ok := closingCallParen(input, match[1])
		if !ok {
			continue
		}
		calls = append(calls, nestedToolCall{Name: input[match[2]:match[3]], Arguments: strings.TrimSpace(input[match[1]:end])})
	}
	return calls
}

// closingCallParen balances nested parentheses while ignoring quoted JavaScript text.
func closingCallParen(input string, start int) (int, bool) {
	depth := 1
	var quote byte
	escaped := false
	for index := start; index < len(input); index++ {
		char := input[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if char == '\\' {
				escaped = true
				continue
			}
			if char == quote {
				quote = 0
			}
			continue
		}
		switch char {
		case '\'', '"', '`':
			quote = char
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return index, true
			}
		}
	}
	return 0, false
}

func nestedToolNames(input string) []string {
	calls := nestedToolCalls(input)
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		names = append(names, call.Name)
	}
	return names
}
