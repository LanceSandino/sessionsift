package transcript

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodingHelpers(t *testing.T) {
	cases := map[string]int64{`12`: 12, `"34"`: 34, `5.9`: 5, `-3`: 0, `-2.5`: 0, `true`: 0, `null`: 0, `"abc"`: 0}
	for input, want := range cases {
		var value count
		if err := json.Unmarshal([]byte(input), &value); err != nil || int64(value) != want {
			t.Errorf("count(%s)=%d err=%v want %d", input, value, err, want)
		}
	}
	if compactJSON(json.RawMessage("{bad")) != "{bad" || compactJSON(nil) != "" {
		t.Fatal("compactJSON fallback")
	}
	if text, blocks := decodeContent(nil); text != "" || blocks != nil || first("", "") != "" {
		t.Fatal("empty content decoded")
	}
	if text, blocks := decodeContent(json.RawMessage(`42`)); text != "" || blocks != nil {
		t.Fatal("non-content decoded")
	}
	// A drifted field keeps the block; a non-object element decodes to an
	// untyped block that every consumer ignores.
	if _, blocks := decodeContent(json.RawMessage(`[{"type":"text","text":1},"loose",{"type":"text","text":"ok"}]`)); len(blocks) != 3 || blocks[0].Type != "text" || blocks[1].Type != "" || blocks[2].Text != "ok" {
		t.Fatalf("blocks=%+v", blocks)
	}
	if resultText(json.RawMessage(`[{"type":"image"},{"type":"text","text":"a"}]`)) != "a" {
		t.Fatal("resultText")
	}
	if _, ok := decodeRecord([]byte(`{"type":"user"`)); ok {
		t.Fatal("truncated record accepted")
	}
	if !parseTime("bad").IsZero() || parseTime("2026-01-01T00:00:00.5+02:00").Location() != time.UTC {
		t.Fatal("parseTime")
	}
	if label("  ") != "untyped" || label("x") != "x" {
		t.Fatal("label")
	}
	if injectedOrigin(json.RawMessage(`{"kind":"Peer-Agent"}`), "hi") != "peer_agent" || injectedOrigin(nil, "plain") != "" {
		t.Fatal("injectedOrigin")
	}
}
