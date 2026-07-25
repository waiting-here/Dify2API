package translator

import (
	"fmt"
	"strings"

	"dify2api/openai"
)

// slotNames lists the Dify App input variables in template order.
// The Dify App prompt template assigns a fixed role to each variable:
//
//	[0]  system_prompt (system)
//	[1]  user_0        (user)
//	[2]  assistant_1   (assistant)
//	[3]  user_1        (user)
//	… alternating assistant/user through [21] user_10
//
// Messages are matched to slots by role: each message consumes the next
// available slot whose role matches, skipping slots of other roles.
var slotNames = []string{
	"system_prompt",
	"user_0",
	"assistant_1", "user_1",
	"assistant_2", "user_2",
	"assistant_3", "user_3",
	"assistant_4", "user_4",
	"assistant_5", "user_5",
	"assistant_6", "user_6",
	"assistant_7", "user_7",
	"assistant_8", "user_8",
	"assistant_9", "user_9",
	"assistant_10", "user_10",
}

// slotRole returns the Dify App prompt-template role of a slot variable.
func slotRole(name string) string {
	switch {
	case name == "system_prompt":
		return "system"
	case strings.HasPrefix(name, "user_"):
		return "user"
	default: // assistant_N
		return "assistant"
	}
}

// TranslateToSlots maps an OpenAI messages array onto the Dify App's input
// variables.  Messages are matched to slots by role (not position): each
// message consumes the next available slot in template order whose role
// matches, skipping over slots of the other role.
//
// The first message may be "system" (optional). If present, it fills
// system_prompt and slot scanning starts at index 1; otherwise system_prompt
// stays empty and scanning starts at index 0 (first message matches user_0).
// Consecutive messages of the same role are fine — they simply consume
// consecutive slots of that role, leaving intermediate slots empty.
// Content is trimmed (whitespace-only becomes empty string).
//
// Valid range: 1–22 messages.  An error is returned when a message cannot
// find a matching slot (all slots of that role consumed).
func TranslateToSlots(messages []openai.Message) (map[string]string, error) {
	if len(messages) < 1 {
		return nil, fmt.Errorf("expected at least 1 message, got %d", len(messages))
	}
	if len(messages) > len(slotNames) {
		return nil, fmt.Errorf("expected at most %d messages, got %d", len(slotNames), len(messages))
	}

	// Initialise all slots empty.
	inputs := make(map[string]string, len(slotNames))
	for _, name := range slotNames {
		inputs[name] = ""
	}

	// system is optional: if first message is system, fill system_prompt
	// and start scanning after it; otherwise leave system_prompt empty.
	slotIdx := 0
	if messages[0].Role == "system" {
		inputs["system_prompt"] = strings.TrimSpace(string(messages[0].Content))
		slotIdx = 1
	}

	// For remaining messages (or all if no system), scan forward through
	// slots to find the next matching role.
	for mi := 0; mi < len(messages); mi++ {
		if mi == 0 && messages[0].Role == "system" {
			continue // already handled above
		}
		m := messages[mi]
		// Advance to the next slot whose role matches this message.
		for slotIdx < len(slotNames) && slotRole(slotNames[slotIdx]) != m.Role {
			slotIdx++
		}
		if slotIdx >= len(slotNames) {
			return nil, fmt.Errorf("messages[%d]: no remaining %q slot (all consumed)", mi, m.Role)
		}
		inputs[slotNames[slotIdx]] = strings.TrimSpace(string(m.Content))
		slotIdx++ // consume this slot
	}

	return inputs, nil
}
