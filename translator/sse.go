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
	failed       bool
	failMsg      string
}

// Failed reports whether the stream ended in a failure (Dify error event or
// workflow_finished with status "failed").  When true the handler must NOT
// send the [DONE] terminator: OpenAI's API signals mid-stream failures with
// an error frame and no [DONE], which official SDKs turn into an exception.
func (c *StreamConverter) Failed() bool { return c.failed }

// FailMessage returns the upstream error message when Failed() is true
// ("" otherwise). Used for admin log diagnostics.
func (c *StreamConverter) FailMessage() string { return c.failMsg }

// NewStreamConverter creates a new StreamConverter.
func NewStreamConverter(modelName string) *StreamConverter {
	return &StreamConverter{
		chunkID:    fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()/1000%1000000000000),
		modelName:  modelName,
		created:    time.Now().Unix(),
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
		if evt.Data.Status == "failed" {
			// Upstream workflow failed mid-stream.  Emit an OpenAI-style
			// error frame instead of a normal stop chunk (see Failed()).
			c.failed = true
			msg := evt.Data.Error
			if msg == "" {
				msg = "workflow failed"
			}
			c.failMsg = msg
			return &SSEMessage{Data: formatSSEError("[Dify] " + msg)}
		}
		chunk := buildStreamChunk(c.chunkID, c.modelName, c.created, "", "", false, "stop")
		return &SSEMessage{Data: formatSSEChunk(chunk)}

	case "error":
		// Dify emits a dedicated error event on some failure paths.
		c.done = true
		c.failed = true
		msg := evt.Data.Error
		if msg == "" {
			msg = evt.Data.Text
		}
		if msg == "" {
			msg = "upstream error"
		}
		c.failMsg = msg
		return &SSEMessage{Data: formatSSEError("[Dify] " + msg)}

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

// formatSSEError builds an OpenAI-style in-stream error frame
// (`data: {"error": {...}}`).  Official OpenAI SDKs recognise this frame
// and raise an error to the caller.
func formatSSEError(message string) string {
	data, _ := json.Marshal(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "upstream_error",
			"code":    "upstream_error",
		},
	})
	return fmt.Sprintf("data: %s\n\n", string(data))
}

// FormatSSEErrorFrame exposes the in-stream error frame builder to the
// handler (used when the error surfaces on errCh after headers were sent,
// e.g. a connection drop mid-stream).
func FormatSSEErrorFrame(message string) string { return formatSSEError(message) }
