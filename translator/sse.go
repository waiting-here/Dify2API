package translator

import (
	"encoding/json"
	"fmt"
	"time"

	"dify2api/dify"
)

// SSEMessage holds a single SSE output line ready to write to HTTP response.
type SSEMessage struct {
	Data string
}

// StreamConverter accumulates state while converting Dify Workflow events
// to OpenAI SSE format. Use NewStreamConverter to create one, then call
// Convert() for each event.
type StreamConverter struct {
	chunkID      string
	modelName    string
	created      int64
	firstChunk   bool
	receivedText bool
	done         bool
}

// NewStreamConverter creates a new StreamConverter.
func NewStreamConverter(modelName string) *StreamConverter {
	return &StreamConverter{
		chunkID:   fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()/1000%1000000000000),
		modelName: modelName,
		created:   time.Now().Unix(),
		firstChunk: true,
	}
}

// Convert processes a single Dify Workflow StreamEvent and returns an SSE message.
// Returns nil if the event should produce no output.
func (c *StreamConverter) Convert(evt dify.StreamEvent) *SSEMessage {
	switch evt.Event {
	case "text_chunk":
		if evt.Data.Text == "" {
			return nil
		}
		c.receivedText = true
		chunk := buildStreamChunk(c.chunkID, c.modelName, c.created, evt.Data.Text, "", c.firstChunk, "")
		c.firstChunk = false
		return &SSEMessage{Data: formatSSEChunk(chunk)}

	case "reasoning_chunk":
		// Map Dify reasoning_chunk → OpenAI delta.reasoning_content
		if evt.Data.Reasoning == "" {
			return nil
		}
		chunk := buildStreamChunk(c.chunkID, c.modelName, c.created, "", evt.Data.Reasoning, c.firstChunk, "")
		c.firstChunk = false
		return &SSEMessage{Data: formatSSEChunk(chunk)}

	case "workflow_finished":
		c.done = true
		chunk := buildStreamChunk(c.chunkID, c.modelName, c.created, "", "", false, "stop")
		return &SSEMessage{Data: formatSSEChunk(chunk)}

	case "node_started", "node_finished", "workflow_started", "ping":
		return nil

	default:
		return nil
	}
}

// Finalize returns the final stop chunk if the stream ended without
// a workflow_finished event (safety net).
func (c *StreamConverter) Finalize() []SSEMessage {
	if c.done || !c.receivedText {
		return nil
	}
	chunk := buildStreamChunk(c.chunkID, c.modelName, c.created, "", "", false, "stop")
	return []SSEMessage{
		{Data: formatSSEChunk(chunk)},
	}
}

func buildStreamChunk(id, model string, created int64, content, reasoning string, includeRole bool, finishReason string) map[string]interface{} {
	delta := map[string]interface{}{}
	if includeRole {
		delta["role"] = "assistant"
	}
	if content != "" {
		delta["content"] = content
	}
	if reasoning != "" {
		delta["reasoning_content"] = reasoning
	}

	choice := map[string]interface{}{
		"index": 0,
		"delta": delta,
	}
	if finishReason != "" {
		choice["finish_reason"] = finishReason
	} else {
		choice["finish_reason"] = nil
	}

	return map[string]interface{}{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []interface{}{choice},
	}
}

func formatSSEChunk(chunk map[string]interface{}) string {
	data, _ := json.Marshal(chunk)
	return fmt.Sprintf("data: %s\n\n", string(data))
}
