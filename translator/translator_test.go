package translator

import (
	"fmt"
	"strings"
	"testing"

	"dify2api/openai"
)

func TestTranslateToSlots_Minimal(t *testing.T) {
	// Shortest valid request with system: system + user only.
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
	emptySlots := []string{
		"assistant_1", "user_1", "assistant_2", "user_2",
		"assistant_3", "user_3", "assistant_4", "user_4",
		"assistant_5", "user_5", "assistant_6", "user_6",
		"assistant_7", "user_7", "assistant_8", "user_8",
		"assistant_9", "user_9", "assistant_10", "user_10",
	}
	for _, name := range emptySlots {
		v, ok := inputs[name]
		if !ok {
			t.Errorf("slot %q missing from result", name)
		} else if v != "" {
			t.Errorf("slot %q = %q, want empty", name, v)
		}
	}
}

func TestTranslateToSlots_NoSystem(t *testing.T) {
	// Single user message, no system — should pass (S is optional).
	messages := []openai.Message{
		{Role: "user", Content: "Hello!"},
	}

	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if inputs["system_prompt"] != "" {
		t.Errorf("system_prompt = %q, want empty", inputs["system_prompt"])
	}
	if inputs["user_0"] != "Hello!" {
		t.Errorf("user_0 = %q, want Hello!", inputs["user_0"])
	}
}

func TestTranslateToSlots_Full(t *testing.T) {
	// Longest valid request: all 22 slots filled.
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
		{Role: "user", Content: "U5"},
		{Role: "assistant", Content: "A6"},
		{Role: "user", Content: "U6"},
		{Role: "assistant", Content: "A7"},
		{Role: "user", Content: "U7"},
		{Role: "assistant", Content: "A8"},
		{Role: "user", Content: "U8"},
		{Role: "assistant", Content: "A9"},
		{Role: "user", Content: "U9"},
		{Role: "assistant", Content: "A10"},
		{Role: "user", Content: "U10"},
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
		"assistant_5":   "A5",
		"user_5":        "U5",
		"assistant_6":   "A6",
		"user_6":        "U6",
		"assistant_7":   "A7",
		"user_7":        "U7",
		"assistant_8":   "A8",
		"user_8":        "U8",
		"assistant_9":   "A9",
		"user_9":        "U9",
		"assistant_10":  "A10",
		"user_10":       "U10",
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
	emptySlots := []string{
		"assistant_2", "user_2", "assistant_3", "user_3",
		"assistant_4", "user_4", "assistant_5", "user_5",
		"assistant_6", "user_6", "assistant_7", "user_7",
		"assistant_8", "user_8", "assistant_9", "user_9",
		"assistant_10", "user_10",
	}
	for _, name := range emptySlots {
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
	// 0 messages should error.
	messages := []openai.Message{}
	if _, err := TranslateToSlots(messages); err == nil {
		t.Fatal("expected error for 0 messages")
	} else if !strings.Contains(err.Error(), "at least 1") {
		t.Errorf("error = %v", err)
	}
}

func TestTranslateToSlots_TooMany(t *testing.T) {
	// 23 messages (22 slots + 1) should error.
	messages := make([]openai.Message, 23)
	messages[0] = openai.Message{Role: "system", Content: "S"}
	for i := 1; i < 23; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		messages[i] = openai.Message{Role: role, Content: "X"}
	}
	if _, err := TranslateToSlots(messages); err == nil {
		t.Fatal("expected error for 23 messages")
	} else if !strings.Contains(err.Error(), "at most 22") {
		t.Errorf("error = %v", err)
	}
}

func TestTranslateToSlots_WrongRole(t *testing.T) {
	// system is optional; if present it must be first (no system slot later).
	// Others match by role, not position.

	t.Run("system in non-first position fails", func(t *testing.T) {
		// [user, system] → user fills user_0, then system can't find a slot
		// (system_prompt at index 0 already passed by).
		messages := []openai.Message{
			{Role: "user", Content: "U0"},
			{Role: "system", Content: "S"},
		}
		_, err := TranslateToSlots(messages)
		if err == nil {
			t.Fatal("expected error for system in non-first position")
		}
		if !strings.Contains(err.Error(), "no remaining") {
			t.Errorf("error %q should mention 'no remaining'", err.Error())
		}
	})

	t.Run("consecutive assistants skip user slots", func(t *testing.T) {
		// [system, assistant, assistant, user] →
		//   system_prompt, assistant_1, assistant_2, user_2
		//   (user_0 and user_1 are skipped)
		messages := []openai.Message{
			{Role: "system", Content: "S"},
			{Role: "assistant", Content: "A0"},
			{Role: "assistant", Content: "A1"},
			{Role: "user", Content: "U"},
		}
		inputs, err := TranslateToSlots(messages)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if inputs["system_prompt"] != "S" {
			t.Errorf("system_prompt = %q", inputs["system_prompt"])
		}
		if inputs["user_0"] != "" {
			t.Errorf("user_0 = %q, want empty", inputs["user_0"])
		}
		if inputs["assistant_1"] != "A0" {
			t.Errorf("assistant_1 = %q, want A0", inputs["assistant_1"])
		}
		if inputs["user_1"] != "" {
			t.Errorf("user_1 = %q, want empty", inputs["user_1"])
		}
		if inputs["assistant_2"] != "A1" {
			t.Errorf("assistant_2 = %q, want A1", inputs["assistant_2"])
		}
		if inputs["user_2"] != "U" {
			t.Errorf("user_2 = %q, want U", inputs["user_2"])
		}
	})

	t.Run("trailing assistant with no more slots", func(t *testing.T) {
		// Fill assistant_1..assistant_10 (all 10 assistant slots), then 11th → error.
		messages := []openai.Message{
			{Role: "system", Content: "S"},
		}
		for i := 1; i <= 11; i++ {
			messages = append(messages, openai.Message{
				Role: "assistant", Content: openai.MessageContent(fmt.Sprintf("A%d", i)),
			})
		}
		_, err := TranslateToSlots(messages)
		if err == nil {
			t.Fatal("expected error for 11th assistant")
		}
		if !strings.Contains(err.Error(), "no remaining") {
			t.Errorf("error = %v", err)
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

func TestTranslateToSlots_ConsecutiveUsers(t *testing.T) {
	// [system, user, user] → system_prompt, user_0, user_1 (assistant_1 skipped).
	messages := []openai.Message{
		{Role: "system", Content: "S"},
		{Role: "user", Content: "U0"},
		{Role: "user", Content: "U1"},
	}
	inputs, err := TranslateToSlots(messages)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["system_prompt"] != "S" || inputs["user_0"] != "U0" || inputs["user_1"] != "U1" {
		t.Errorf("inputs = %v", inputs)
	}
	if inputs["assistant_1"] != "" {
		t.Errorf("assistant_1 = %q, want empty (skipped)", inputs["assistant_1"])
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
