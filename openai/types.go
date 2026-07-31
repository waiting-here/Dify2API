package openai

import (
	"encoding/json"
	"fmt"
)

// ChatCompletionRequest is the OpenAI-compatible request body for /v1/chat/completions.
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// User is an optional end-user identifier (not the same as Dify's "user").
	User string `json:"user,omitempty"`
}

// Message represents a single message in the conversation.
type Message struct {
	Role    string         `json:"role"`    // "system", "user", "assistant"
	Content MessageContent `json:"content"` // text content (string or text parts)
	// Images holds image references from OpenAI multimodal content parts:
	// either "data:image/...;base64,..." data URIs or http(s) URLs.
	Images []string `json:"-"`
}

// UnmarshalJSON parses content as either a plain string or an array of parts
// (text and image_url). Text parts are concatenated; image_url URLs are
// collected into Images.
func (m *Message) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role

	// Plain string content.
	var s string
	if err := json.Unmarshal(raw.Content, &s); err == nil {
		m.Content = MessageContent(s)
		return nil
	}

	// Multimodal content parts.
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL *struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(raw.Content, &parts); err != nil {
		return fmt.Errorf("content must be a string or an array of content parts: %w", err)
	}
	var text string
	for _, p := range parts {
		switch p.Type {
		case "text":
			text += p.Text
		case "image_url":
			if p.ImageURL != nil && p.ImageURL.URL != "" {
				m.Images = append(m.Images, p.ImageURL.URL)
			}
		}
	}
	m.Content = MessageContent(text)
	return nil
}

// MessageContent handles content that can be either a plain string
// (e.g., "Hello") or a multimodal array (e.g., [{"type":"text","text":"Hello"}]).
type MessageContent string

// UnmarshalJSON implements json.Unmarshaler to accept both string and array formats.
func (mc *MessageContent) UnmarshalJSON(data []byte) error {
	// Try as plain string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*mc = MessageContent(s)
		return nil
	}

	// Try as content-parts array: [{"type":"text","text":"..."}, ...]
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("content must be a string or an array of content parts: %w", err)
	}

	// Concatenate all text parts
	var result string
	for _, p := range parts {
		if p.Type == "text" {
			result += p.Text
		}
	}
	*mc = MessageContent(result)
	return nil
}

// ChatCompletionResponse is the non-streaming response (for completeness).
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"` // "chat.completion"
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage,omitempty"`
}

// Choice is a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        Delta   `json:"delta,omitempty"`
	FinishReason string  `json:"finish_reason"` // "stop", "length", or null
}

// Delta is the streaming delta content.
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// Usage holds token usage statistics.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ModelListResponse is the response for GET /v1/models.
type ModelListResponse struct {
	Object string  `json:"object"` // "list"
	Data   []Model `json:"data"`
}

// Model represents a single model entry.
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"` // "model"
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}
