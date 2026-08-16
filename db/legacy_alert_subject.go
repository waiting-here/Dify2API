package db

import (
	"strconv"
	"strings"
)

// legacyAlertSubjectID extracts the user ID from the pre-R05 mailer message
// format. The username is untrusted and may contain ID-looking text, so the
// token must be the final one immediately before the event's fixed body
// separator. A caller must still restrict this parser to the two legacy
// mailer event types.
func legacyAlertSubjectID(alertType, message string) (int64, bool) {
	var separator string
	switch alertType {
	case "user_auto_banned":
		separator = "）因 "
	case "debug_abuse":
		separator = "）在 "
	default:
		return 0, false
	}

	separatorAt := strings.LastIndex(message, separator)
	if separatorAt < 0 {
		return 0, false
	}
	prefix := message[:separatorAt]
	const marker = "（ID："
	markerAt := strings.LastIndex(prefix, marker)
	if markerAt < 0 {
		return 0, false
	}
	token := prefix[markerAt+len(marker):]
	if token == "" {
		return 0, false
	}
	for i := 0; i < len(token); i++ {
		if token[i] < '0' || token[i] > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(token, 10, 63)
	if err != nil || value == 0 {
		return 0, false
	}
	return int64(value), true
}
