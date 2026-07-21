package translator

import (
	"fmt"
	"strings"

	"dify2api/openai"
)

// slotNames lists the Dify App input variables in message order.
// The Dify App expects this exact message layout:
//
//	messages[0] system    -> system_prompt
//	messages[1] user      -> user_0
//	messages[2] assistant -> assistant_1
//	messages[3] user      -> user_1
//	messages[4] assistant -> assistant_2
//	messages[5] user      -> user_2
//	messages[6] assistant -> assistant_3
//	messages[7] user      -> user_3
var slotNames = []string{
	"system_prompt",
	"user_0",
	"assistant_1", "user_1",
	"assistant_2", "user_2",
	"assistant_3", "user_3",
}

// expectedRole returns the role a conforming request must have at index i:
// system first, then user/assistant alternating starting with user.
func expectedRole(i int) string {
	switch {
	case i == 0:
		return "system"
	case i%2 == 1:
		return "user"
	default:
		return "assistant"
	}
}

// TranslateToSlots maps a strictly formatted OpenAI messages array onto the
// Dify App's input variables. The expected layout is:
//
//	system, user, then 0-3 pairs of assistant, user   (2-8 messages total)
//
// All eight variables are always present in the result; messages absent from
// the optional tail leave their variables as empty strings.
// A non-conforming request returns an error naming the first violation.
func TranslateToSlots(messages []openai.Message) (map[string]string, error) {
	if len(messages) < 2 {
		return nil, fmt.Errorf("expected at least 2 messages (system, user), got %d", len(messages))
	}
	if len(messages) > len(slotNames) {
		return nil, fmt.Errorf("expected at most %d messages (system, user, then up to 3 assistant/user pairs), got %d", len(slotNames), len(messages))
	}

	inputs := make(map[string]string, len(slotNames))
	for _, name := range slotNames {
		inputs[name] = ""
	}

	for i, m := range messages {
		if want := expectedRole(i); m.Role != want {
			return nil, fmt.Errorf("messages[%d]: expected role %q, got %q (expected layout: system, user, then assistant/user pairs)", i, want, m.Role)
		}
		inputs[slotNames[i]] = strings.TrimSpace(string(m.Content))
	}

	if inputs["user_0"] == "" {
		return nil, fmt.Errorf("messages[1] (user_0) must not be empty")
	}

	return inputs, nil
}
