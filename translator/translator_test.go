package translator

import (
	"strings"
	"testing"

	"dify2api/openai"
)

func TestTranslateToSlots_Minimal(t *testing.T) {
	// Shortest valid request: system + user only.
	messages := []openai.Message{
		{Role: "system", Content: "You are a pirate."},
		{Role: "user", Content: "Ahoy!"},
	}

	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inputs["system_prompt"] != "You are a pirate." {
		t.Errorf("system_prompt = %q", inputs["system_prompt"])
	}
	if inputs["user_0"] != "Ahoy!" {
		t.Errorf("user_0 = %q", inputs["user_0"])
	}
	// All optional slots must be present but empty.
	for _, name := range []string{"assistant_1", "user_1", "assistant_2", "user_2", "assistant_3", "user_3", "assistant_4", "user_4"} {
		v, ok := inputs[name]
		if !ok {
			t.Errorf("slot %q missing from result", name)
		} else if v != "" {
			t.Errorf("slot %q = %q, want empty", name, v)
		}
	}
}

func TestTranslateToSlots_Full(t *testing.T) {
	// Longest valid request: all 10 slots filled.
	messages := []openai.Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U0"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "U1"},
		{Role: "assistant", Content: "A2"},
		{Role: "user", Content: "U2"},
		{Role: "assistant", Content: "A3"},
		{Role: "user", Content: "U3"},
		{Role: "assistant", Content: "A4"},
		{Role: "user", Content: "U4"},
	}

	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"system_prompt": "S",
		"user_0":        "U0",
		"assistant_1":   "A1",
		"user_1":        "U1",
		"assistant_2":   "A2",
		"user_2":        "U2",
		"assistant_3":   "A3",
		"user_3":        "U3",
		"assistant_4":   "A4",
		"user_4":        "U4",
	}
	for k, v := range want {
		if inputs[k] != v {
			t.Errorf("inputs[%q] = %q, want %q", k, inputs[k], v)
		}
	}
}

func TestTranslateToSlots_PartialPairs(t *testing.T) {
	// One pair present, remaining pairs left empty.
	messages := []openai.Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U0"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "U1"},
	}

	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["assistant_1"] != "A1" || inputs["user_1"] != "U1" {
		t.Errorf("pair 1 wrong: assistant_1=%q user_1=%q", inputs["assistant_1"], inputs["user_1"])
	}
	for _, name := range []string{"assistant_2", "user_2", "assistant_3", "user_3", "assistant_4", "user_4"} {
		if inputs[name] != "" {
			t.Errorf("slot %q = %q, want empty", name, inputs[name])
		}
	}
}

func TestTranslateToSlots_TrailingAssistant(t *testing.T) {
	// A request may end on an assistant message (prefill-style);
	// the unmatched user slot stays empty.
	messages := []openai.Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U0"},
		{Role: "assistant", Content: "A1"},
	}

	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["assistant_1"] != "A1" {
		t.Errorf("assistant_1 = %q", inputs["assistant_1"])
	}
	if inputs["user_1"] != "" {
		t.Errorf("user_1 = %q, want empty", inputs["user_1"])
	}
}

func TestTranslateToSlots_TooFew(t *testing.T) {
	messages := []openai.Message{
		{Role: "system", Content: "S"},
	}
	if _, err := TranslateToSlots(messages); err == nil {
		t.Fatal("expected error for 1 message")
	} else if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("error = %v", err)
	}
}

func TestTranslateToSlots_TooMany(t *testing.T) {
	messages := []openai.Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U0"},
		{Role: "assistant", Content: "A1"},
		{Role: "user", Content: "U1"},
		{Role: "assistant", Content: "A2"},
		{Role: "user", Content: "U2"},
		{Role: "assistant", Content: "A3"},
		{Role: "user", Content: "U3"},
		{Role: "assistant", Content: "A4"},
		{Role: "user", Content: "U4"},
		{Role: "assistant", Content: "A5"},
	}
	if _, err := TranslateToSlots(messages); err == nil {
		t.Fatal("expected error for 11 messages")
	} else if !strings.Contains(err.Error(), "at most 10") {
		t.Errorf("error = %v", err)
	}
}

func TestTranslateToSlots_WrongRole(t *testing.T) {
	// Only messages[0] is role-checked.  All other positions fill positionally.

	t.Run("first must be system", func(t *testing.T) {
		messages := []openai.Message{
			{Role: "user", Content: "U0"},
			{Role: "user", Content: "U1"},
		}
		_, err := TranslateToSlots(messages)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "messages[0]") {
			t.Errorf("error %q should mention messages[0]", err.Error())
		}
	})

	t.Run("non-system at 0 passes positional fill", func(t *testing.T) {
		// All non-[0] messages are accepted regardless of role.
		messages := []openai.Message{
			{Role: "system", Content: "S"},
			{Role: "assistant", Content: "A0"}, // would have been illegal before
			{Role: "assistant", Content: "A1"}, // consecutive assistant → fills assistant_1,assistant_2
		}
		inputs, err := TranslateToSlots(messages)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inputs["user_0"] != "A0" {
			t.Errorf("user_0 = %q, want A0 (positional fill)", inputs["user_0"])
		}
		if inputs["assistant_1"] != "A1" {
			t.Errorf("assistant_1 = %q, want A1", inputs["assistant_1"])
		}
		// user_1 should be empty (no 4th message).
		if inputs["user_1"] != "" {
			t.Errorf("user_1 = %q, want empty", inputs["user_1"])
		}
	})
}

func TestTranslateToSlots_EmptySlots(t *testing.T) {
	// Empty / whitespace content is accepted (all slots are optional).
	messages := []openai.Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "   "},
	}
	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["system_prompt"] != "S" {
		t.Errorf("system_prompt = %q", inputs["system_prompt"])
	}
	// Trimmed whitespace → empty string.
	if inputs["user_0"] != "" {
		t.Errorf("user_0 = %q, want empty after trim", inputs["user_0"])
	}
}

func TestTranslateToSlots_TrimsContent(t *testing.T) {
	messages := []openai.Message{
		{Role: "system", Content: "  S  "},
		{Role: "user", Content: "\n U0 \n"},
	}
	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["system_prompt"] != "S" || inputs["user_0"] != "U0" {
		t.Errorf("content not trimmed: %q / %q", inputs["system_prompt"], inputs["user_0"])
	}
}
