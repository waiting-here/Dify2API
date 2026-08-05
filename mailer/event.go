package mailer

import "fmt"

// EventType identifies the kind of operational alert.
type EventType string

const (
	EventUserAutoBanned   EventType = "user_auto_banned"
	EventDonationInactive EventType = "donation_inactive"
	EventAdminLoginLocked EventType = "admin_login_locked"
	EventPricingMissing   EventType = "pricing_missing"
	EventDebugAbuse       EventType = "debug_abuse"
)

// eventSubject returns the Chinese subject line for the aggregated email.
func eventSubject(et EventType, count int) string {
	var label string
	switch et {
	case EventUserAutoBanned:
		label = "用户自动封禁"
	case EventDonationInactive:
		label = "捐赠条目自动未激活"
	case EventAdminLoginLocked:
		label = "管理员登录锁定"
	case EventPricingMissing:
		label = "公益定价缺失"
	case EventDebugAbuse:
		label = "用户 Debug 滥用告警"
	default:
		label = "系统通知"
	}
	return fmt.Sprintf("【Dify2API】%s（%d 起）", label, count)
}

// AllEventTypes returns every known email event category. The gateway seeds
// alert_prefs rows from this list so each category gets its own switches.
func AllEventTypes() []EventType {
	return []EventType{
		EventUserAutoBanned,
		EventDonationInactive,
		EventAdminLoginLocked,
		EventPricingMissing,
		EventDebugAbuse,
	}
}
