// @module MCPSetupFormat preserves unrelated config and rejects ambiguous client syntax.
package mcpsetup

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func proposed(o Options, before []byte) (string, json.RawMessage, []byte, error) {
	snippet, entry := entryFor(o)
	if o.Client == "codex" {
		if e := tomlSafe(before); e != nil {
			return "", nil, nil, e
		}
		prefix := append([]byte(nil), before...)
		if len(prefix) > 0 && prefix[len(prefix)-1] != '\n' {
			prefix = append(prefix, '\n')
		}
		return snippet, nil, append(prefix, []byte(snippet)...), nil
	}
	root, servers, e := jsonConfig(before)
	if e != nil {
		return "", nil, nil, e
	}
	if _, exists := servers["sessionsift"]; exists {
		return "", nil, nil, errors.New("an existing sessionsift MCP entry must be reviewed manually")
	}
	servers["sessionsift"] = entry
	encoded, _ := json.Marshal(servers)
	root["mcpServers"] = encoded
	after, e := json.MarshalIndent(root, "", "  ")
	return snippet, entry, append(after, '\n'), e
}

func removeOwned(o Options, before []byte, owned receipt) ([]byte, error) {
	if o.Client == "codex" {
		if strings.Count(string(before), owned.Snippet) != 1 {
			return nil, errors.New("owned Codex block missing, duplicated or modified; refusing undo")
		}
		after := bytes.Replace(before, []byte(owned.Snippet), nil, 1)
		if e := tomlSafe(after); e != nil {
			return nil, e
		}
		return after, nil
	}
	root, servers, e := jsonConfig(before)
	if e != nil {
		return nil, e
	}
	entry, exists := servers["sessionsift"]
	if !exists || !equalJSON(entry, owned.Entry) {
		return nil, errors.New("owned Claude entry missing or modified; refusing undo")
	}
	delete(servers, "sessionsift")
	encoded, _ := json.Marshal(servers)
	root["mcpServers"] = encoded
	after, e := json.MarshalIndent(root, "", "  ")
	return append(after, '\n'), e
}

// This is intentionally not a TOML parser. Ambiguous alternate MCP table/key
// forms and multiline strings require manual setup instead of speculative edits.
func tomlSafe(data []byte) error {
	if !bytes.Equal(bytes.ToValidUTF8(data, nil), data) {
		return errors.New("invalid UTF-8 config")
	}
	if bytes.Contains(data, []byte(`"""`)) || bytes.Contains(data, []byte(`'''`)) {
		return errors.New("multiline TOML requires manual setup")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Removing quotes and spaces deliberately over-rejects suspicious names.
		canonical := strings.NewReplacer(" ", "", "\t", "", "\"", "", "'", "").Replace(line)
		if strings.HasPrefix(canonical, "mcp_servers=") || strings.HasPrefix(canonical, "mcp_servers.") || strings.HasPrefix(canonical, "[mcp_servers.sessionsift") || strings.HasPrefix(canonical, "[[mcp_servers") || strings.HasPrefix(canonical, "[mcp_servers]") {
			return errors.New("conflicting or ambiguous MCP TOML requires manual review")
		}
		if strings.Contains(canonical, "\\u") || strings.Contains(canonical, "\\U") {
			return errors.New("escaped TOML keys require manual setup")
		}
	}
	return nil
}

func jsonConfig(data []byte) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	if !bytes.Equal(bytes.ToValidUTF8(data, nil), data) {
		return nil, nil, errors.New("invalid UTF-8 config")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if e := checkJSONValue(decoder); e != nil {
		return nil, nil, e
	}
	if _, e := decoder.Token(); e != io.EOF {
		return nil, nil, errors.New("trailing JSON input")
	}
	var root map[string]json.RawMessage
	if e := json.Unmarshal(data, &root); e != nil || root == nil {
		return nil, nil, errors.New("client config must be a JSON object")
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := root["mcpServers"]; ok {
		if e := json.Unmarshal(raw, &servers); e != nil || servers == nil {
			return nil, nil, errors.New("mcpServers must be a JSON object")
		}
	}
	return root, servers, nil
}
func checkJSONValue(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	delimiter, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			if e != nil {
				return e
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errors.New("duplicate or invalid JSON key")
			}
			seen[name] = true
			if e = checkJSONValue(d); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e = checkJSONValue(d); e != nil {
				return e
			}
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	_, e = d.Token()
	return e
}
