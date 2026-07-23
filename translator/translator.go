package translator

import (
	"fmt"
	"strings"

	"dify2api/openai"
)

// slotNames lists the Dify App input variables in template order.
// The Dify App prompt template assigns a fixed role to each variable:
//
//	[0] system_prompt (system)
//	[1] user_0        (user)
//	[2] assistant_1   (assistant)
//	[3] user_1        (user)
//	… alternating assistant/user through [9] user_4
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
// The only hard constraint is messages[0] must be "system".  After that,
// consecutive messages of the same role are fine — they simply consume
// consecutive slots of that role, leaving intermediate slots empty.
// Content is trimmed (whitespace-only becomes empty string).
//
// Valid range: 2–10 messages.  An error is returned when a message cannot
// find a matching slot (all slots of that role consumed).
func TranslateToSlots(messages []openai.Message) (map[string]string, error) {
	if len(messages) < 2 {
		return nil, fmt.Errorf("expected at least 2 messages (system + 1), got %d", len(messages))
	}
	if len(messages) > len(slotNames) {
		return nil, fmt.Errorf("expected at most %d messages, got %d", len(slotNames), len(messages))
	}

	if messages[0].Role != "system" {
		return nil, fmt.Errorf("messages[0]: expected role \"system\", got %q", messages[0].Role)
	}

	// Initialise all slots empty.
	inputs := make(map[string]string, len(slotNames))
	for _, name := range slotNames {
		inputs[name] = ""
	}

	// messages[0] → system_prompt (only system slot).
	inputs["system_prompt"] = strings.TrimSpace(string(messages[0].Content))

	// For remaining messages, scan forward through slots to find the next
	// matching role.
	slotIdx := 1 // start after system_prompt
	for mi := 1; mi < len(messages); mi++ {
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
