package translator

import (
	"strings"
	"testing"

	"dify2api/dify"
)

func TestStreamConverter_TextAndFinish(t *testing.T) {
	c := NewStreamConverter("m")
	msg := c.Convert(dify.StreamEvent{Event: "text_chunk", Data: dify.StreamEventData{Text: "hello"}})
	if msg == nil || !strings.Contains(msg.Data, `"content":"hello"`) {
		t.Fatalf("text chunk not converted: %v", msg)
	}
	msg = c.Convert(dify.StreamEvent{Event: "workflow_finished", Data: dify.StreamEventData{Status: "succeeded"}})
	if msg == nil || !strings.Contains(msg.Data, `"finish_reason":"stop"`) {
		t.Fatalf("finish chunk wrong: %v", msg)
	}
	if c.Failed() {
		t.Error("successful stream must not be marked failed")
	}
	if fin := c.Finalize(); fin != nil {
		t.Errorf("Finalize after workflow_finished should be nil, got %v", fin)
	}
}

func TestStreamConverter_WorkflowFailed(t *testing.T) {
	c := NewStreamConverter("m")
	c.Convert(dify.StreamEvent{Event: "text_chunk", Data: dify.StreamEventData{Text: "partial"}})
	msg := c.Convert(dify.StreamEvent{Event: "workflow_finished", Data: dify.StreamEventData{Status: "failed", Error: "credit exhausted"}})
	if msg == nil {
		t.Fatal("failed workflow_finished should emit an error frame")
	}
	if !strings.Contains(msg.Data, `"error"`) || !strings.Contains(msg.Data, "credit exhausted") ||
		!strings.Contains(msg.Data, "[Dify]") {
		t.Errorf("error frame wrong: %s", msg.Data)
	}
	if strings.Contains(msg.Data, "finish_reason") {
		t.Errorf("error frame must not carry finish_reason: %s", msg.Data)
	}
	if !c.Failed() {
		t.Error("converter should be marked failed")
	}
}

func TestStreamConverter_ErrorEvent(t *testing.T) {
	c := NewStreamConverter("m")
	msg := c.Convert(dify.StreamEvent{Event: "error", Data: dify.StreamEventData{Error: "boom"}})
	if msg == nil || !strings.Contains(msg.Data, "boom") || !strings.Contains(msg.Data, `"upstream_error"`) {
		t.Fatalf("error event frame wrong: %v", msg)
	}
	if !c.Failed() {
		t.Error("converter should be marked failed")
	}
}

func TestStreamConverter_ErrorEventEmptyMessage(t *testing.T) {
	c := NewStreamConverter("m")
	msg := c.Convert(dify.StreamEvent{Event: "error"})
	if msg == nil || !strings.Contains(msg.Data, "upstream error") {
		t.Fatalf("empty error event should fall back to generic message: %v", msg)
	}
}

func TestFormatSSEErrorFrame(t *testing.T) {
	frame := FormatSSEErrorFrame("[Dify] conn reset")
	if !strings.HasPrefix(frame, "data: ") || !strings.HasSuffix(frame, "\n\n") {
		t.Errorf("frame not SSE-formatted: %q", frame)
	}
	if !strings.Contains(frame, "conn reset") || !strings.Contains(frame, `"code":"upstream_error"`) {
		t.Errorf("frame content wrong: %q", frame)
	}
}
