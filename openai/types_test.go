package openai

import (
	"encoding/json"
	"testing"
)

// Test JSON serialization of a typical streaming request
func TestChatCompletionRequest_JSON(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "dify-chatflow",
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello!"},
		},
		Stream:      true,
		Temperature: 0.7,
		MaxTokens:   2000,
		User:        "user-001",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded ChatCompletionRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(decoded.Messages) != 2 {
		t.Errorf("Messages len = %d, want 2", len(decoded.Messages))
	}
	if decoded.Messages[0].Role != "system" {
		t.Errorf("Messages[0].Role = %q, want %q", decoded.Messages[0].Role, "system")
	}
	if string(decoded.Messages[1].Content) != "Hello!" {
		t.Errorf("Messages[1].Content = %q, want %q", decoded.Messages[1].Content, "Hello!")
	}
	if !decoded.Stream {
		t.Error("Stream should be true")
	}
}

// Test that empty optional fields are omitted
func TestChatCompletionRequest_OmitEmpty(t *testing.T) {
	req := ChatCompletionRequest{
		Model: "dify-chatflow",
		Messages: []Message{
			{Role: "user", Content: "Hi"},
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// stream should be omitted when false
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, ok := raw["stream"]; ok {
		t.Error("stream should be omitted when false")
	}
	if _, ok := raw["temperature"]; ok {
		t.Error("temperature should be omitted when zero")
	}
}

func TestMessageContent_ArrayFormat(t *testing.T) {
	// Pi and other multimodal clients send content as an array
	body := `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"Hello"},{"type":"text","text":" world"}]}]}`

	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal array content: %v", err)
	}

	if string(req.Messages[0].Content) != "Hello world" {
		t.Errorf("array content = %q, want %q", req.Messages[0].Content, "Hello world")
	}
}

func TestMessageContent_StringFormat(t *testing.T) {
	body := `{"model":"test","messages":[{"role":"user","content":"Hi there"}]}`

	var req ChatCompletionRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal string content: %v", err)
	}

	if string(req.Messages[0].Content) != "Hi there" {
		t.Errorf("string content = %q, want %q", req.Messages[0].Content, "Hi there")
	}
}
