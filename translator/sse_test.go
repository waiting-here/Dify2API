package translator

import (
	"encoding/json"
	"strings"
	"testing"

	"dify2api/dify"
)

// helper to create a text_chunk StreamEvent
func mkData(text, reasoning string) dify.StreamEvent {
	return dify.StreamEvent{Event: "text_chunk", Data: dify.StreamEventData{Text: text, Reasoning: reasoning}}
}

func TestStreamConverter_ReasoningContent(t *testing.T) {
	conv := NewStreamConverter("test-model")

	msg := conv.Convert(dify.StreamEvent{Event: "reasoning_chunk", Data: dify.StreamEventData{Reasoning: "Let me think..."}})

	if msg == nil {
		t.Fatal("expected non-nil")
	}
	if !strings.Contains(msg.Data, `"reasoning_content":"Let me think..."`) {
		t.Errorf("missing reasoning_content, got: %s", msg.Data)
	}
	if strings.Contains(msg.Data, `"content"`) {
		t.Error("reasoning chunk should NOT have content field")
	}
}

func TestStreamConverter_TextChunk(t *testing.T) {
	conv := NewStreamConverter("test")

	msg := conv.Convert(mkData("Hello", ""))
	if msg == nil {
		t.Fatal("expected non-nil")
	}
	if !strings.Contains(msg.Data, `"content":"Hello"`) {
		t.Errorf("missing content, got: %s", msg.Data)
	}
	if !strings.Contains(msg.Data, `"role":"assistant"`) {
		t.Errorf("first chunk should have role")
	}

	// second chunk: no role
	msg = conv.Convert(mkData(" world", ""))
	if strings.Contains(msg.Data, `"role":"assistant"`) {
		t.Error("second chunk should NOT have role")
	}
}

func TestStreamConverter_WorkflowFinished(t *testing.T) {
	conv := NewStreamConverter("test")

	conv.Convert(mkData("Hi", ""))
	msg := conv.Convert(dify.StreamEvent{Event: "workflow_finished"})

	if !strings.Contains(msg.Data, `"finish_reason":"stop"`) {
		t.Errorf("missing stop, got: %s", msg.Data)
	}
	if strings.Contains(msg.Data, `"content"`) {
		t.Error("final chunk should not have content")
	}
}

func TestStreamConverter_Finalize(t *testing.T) {
	conv := NewStreamConverter("test")

	if msgs := conv.Finalize(); msgs != nil {
		t.Error("should be nil without text")
	}

	conv.Convert(mkData("test", ""))
	msgs := conv.Finalize()
	if len(msgs) != 1 {
		t.Fatalf("expected 1, got %d", len(msgs))
	}
}

func TestStreamConverter_AfterFinished(t *testing.T) {
	conv := NewStreamConverter("test")

	conv.Convert(mkData("test", ""))
	conv.Convert(dify.StreamEvent{Event: "workflow_finished"})

	if msgs := conv.Finalize(); msgs != nil {
		t.Error("should be nil after workflow_finished")
	}
}

func TestStreamConverter_JSONValidity(t *testing.T) {
	conv := NewStreamConverter("test-model")

	msg := conv.Convert(mkData("Hi", ""))
	payload := strings.TrimPrefix(msg.Data, "data: ")
	payload = strings.TrimSpace(payload)

	var chunk map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if chunk["object"] != "chat.completion.chunk" {
		t.Errorf("object = %v", chunk["object"])
	}
}
