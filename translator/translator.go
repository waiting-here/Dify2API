package translator

import (
	"fmt"
	"strings"

	"dify2api/openai"
)

// slotNames lists the Dify App input variables in message order.
// The Dify App puts these into the prompt template in fixed order:
//
//	[0] system_prompt (system)
//	[1] user_0        (user)
//	[2] assistant_1   (assistant)
//	[3] user_1        (user)
//	… alternating assistant/user through [9] user_4
//
// The translator fills slots positionally from the incoming messages,
// ignoring the client-supplied role for all slots except [0] (system).
var slotNames = []string{
	"system_prompt",
	"user_0",
	"assistant_1", "user_1",
	"assistant_2", "user_2",
	"assistant_3", "user_3",
	"assistant_4", "user_4",
}

// TranslateToSlots maps an OpenAI messages array onto the Dify App's input
// variables using pure positional assignment. Only message [0] is
// role-checked (must be "system").  All other messages are assigned to slots
// in order regardless of their client-supplied role.  Content is trimmed.
//
// Valid range: 2–10 messages (system + 1–9 body messages).
// All 10 slot variables are always present in the result; tail slots not
// covered by the input stay empty.
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

	inputs := make(map[string]string, len(slotNames))
	for _, name := range slotNames {
		inputs[name] = ""
	}

	for i, m := range messages {
		inputs[slotNames[i]] = strings.TrimSpace(string(m.Content))
	}

	return inputs, nil
}
