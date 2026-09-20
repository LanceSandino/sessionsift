// Package toolmeta classifies tool transports without changing observed names.
package toolmeta

import "strings"

// Describe returns the observed transport and server represented by a tool name.
func Describe(name string) (transport, server string) {
	value := strings.TrimSpace(name)
	if strings.HasPrefix(value, "mcp__") {
		parts := strings.Split(value, "__")
		if len(parts) >= 3 {
			return "mcp", parts[1]
		}
		return "mcp", ""
	}
	if strings.HasPrefix(value, "mcp.") {
		parts := strings.Split(value, ".")
		if len(parts) >= 3 {
			return "mcp", parts[1]
		}
		return "mcp", ""
	}
	for _, prefix := range []string{"connector.", "app."} {
		if strings.HasPrefix(value, prefix) {
			parts := strings.Split(value, ".")
			if len(parts) >= 2 {
				return "app", parts[1]
			}
			return "app", ""
		}
	}
	return "builtin", ""
}
