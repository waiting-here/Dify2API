package mailer

import "fmt"

// EventType identifies the kind of operational alert.
type EventType string

const (
	EventUserAutoBanned   EventType = "user_auto_banned"
	EventDonationInactive EventType = "donation_inactive"
	EventAdminLoginLocked EventType = "admin_login_locked"
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
	default:
		label = "系统通知"
	}
	return fmt.Sprintf("【Dify2API】%s（%d 起）", label, count)
}
