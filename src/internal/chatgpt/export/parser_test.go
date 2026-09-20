package export

import (
	"strings"
	"testing"

	"github.com/LanceSandino/sessionsift/src/internal/domain"
)

func TestParseConversationAndMCPToolCall(t *testing.T) {
	input := `[{
  "id":"chat-1","title":"Design a launch","create_time":100,"update_time":106,"current_node":"tool-output",
  "mapping":{
    "root":{"id":"root","parent":null,"children":["user"],"message":null},
    "user":{"id":"user","parent":"root","children":["assistant-call"],"message":{"id":"m1","author":{"role":"user"},"create_time":101,"content":{"content_type":"text","parts":["Make a launch graphic"]},"metadata":{}}},
    "assistant-call":{"id":"assistant-call","parent":"user","children":["tool-output"],"message":{"id":"call-1","author":{"role":"assistant"},"create_time":102,"recipient":"mcp.canva.create_design","content":{"content_type":"text","parts":["{\"format\":\"square\"}"]},"metadata":{"model_slug":"gpt-test"}}},
    "tool-output":{"id":"tool-output","parent":"assistant-call","children":[],"message":{"id":"result-1","author":{"role":"tool","name":"mcp.canva.create_design"},"create_time":103,"content":{"content_type":"text","parts":["{\"ok\":true}"]},"metadata":{}}}
  }
}]`
	sessions, err := Parse(strings.NewReader(input))
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	session := sessions[0]
	if session.Source != domain.SourceChatGPT || session.Kind != domain.ChatSession || session.Title != "Design a launch" || session.Model != "gpt-test" {
		t.Fatalf("session=%+v", session)
	}
	if len(session.Turns) != 1 || session.Turns[0].UserPromptRedacted != "Make a launch graphic" {
		t.Fatalf("turns=%+v", session.Turns)
	}
	if len(session.ToolCalls) != 1 || session.ToolCalls[0].ToolTransport != "mcp" || session.ToolCalls[0].ToolServer != "canva" || session.ToolCalls[0].ResultPreviewRedacted == "" {
		t.Fatalf("tools=%+v", session.ToolCalls)
	}
}

func TestParseUsesCurrentBranchAndRejectsInvalidRoot(t *testing.T) {
	input := `[{
  "id":"chat","title":"Branch","current_node":"chosen",
  "mapping":{
    "root":{"id":"root","children":["chosen","discarded"]},
    "chosen":{"id":"chosen","parent":"root","message":{"id":"one","author":{"role":"user"},"create_time":2,"content":{"parts":["Chosen"]}}},
    "discarded":{"id":"discarded","parent":"root","message":{"id":"two","author":{"role":"user"},"create_time":1,"content":{"parts":["Discarded"]}}}
  }
}]`
	sessions, err := Parse(strings.NewReader(input))
	if err != nil || len(sessions) != 1 || len(sessions[0].Turns) != 1 || sessions[0].Turns[0].UserPromptRedacted != "Chosen" {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	if _, err := Parse(strings.NewReader(`{"not":"an array"}`)); err == nil {
		t.Fatal("expected invalid export root error")
	}
}

func TestParseFallbackOrderAssistantTextAndOrphanTool(t *testing.T) {
	input := `[
 {"id":"","title":"ignored"},
 {"id":"fallback","title":"","create_time":1,"current_node":"missing","mapping":{
   "assistant2":{"message":{"id":"a2","author":{"role":"assistant"},"create_time":3,"content":{"text":"Second answer"}}},
   "tool":{"message":{"id":"tool","author":{"role":"tool","name":"browser"},"create_time":4,"content":{"parts":[{"text":"tool result"}]}}},
   "user":{"message":{"id":"user","author":{"role":"user"},"create_time":1,"content":{"parts":["Question"]}}},
   "assistant1":{"message":{"id":"a1","author":{"role":"assistant"},"create_time":2,"content":{"parts":["First answer"]}}}
 }}]`
	sessions, err := Parse(strings.NewReader(input))
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions=%+v err=%v", sessions, err)
	}
	session := sessions[0]
	if session.Title != "Question" || len(session.Turns) != 1 || session.Turns[0].AssistantSummaryRedacted != "First answer\nSecond answer" {
		t.Fatalf("session=%+v", session)
	}
	if len(session.ToolCalls) != 1 || session.ToolCalls[0].ToolName != "browser" || session.ToolCalls[0].ResultPreviewRedacted != "tool result" {
		t.Fatalf("tools=%+v", session.ToolCalls)
	}
}
