package handler

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"

	"dify2api/db"
	"dify2api/dify"
)

// sanitizePublicUpstreamError is the single user-facing boundary for upstream
// error text. Upstream and stored messages are untrusted: Dify may reflect a
// URL, resolved IP, credential, HTML response body, or other deployment
// detail. Public responses therefore retain only a locally classified,
// localized diagnosis. The original error remains available to request logs,
// admin exports, alerts, and server logs.
func sanitizePublicUpstreamError(err error, raw, lang string) string {
	message := strings.ToLower(strings.TrimSpace(raw))
	if message == "" && err != nil {
		message = strings.ToLower(err.Error())
	}

	if errors.Is(err, context.DeadlineExceeded) || dify.IsTimeoutError(err) ||
		containsAny(message, "timeout", "timed out", "deadline exceeded", "unexpected eof", "connection reset") {
		return t(lang,
			"上游 Dify 服务响应超时，请稍后重试或改用流式传输。",
			"The upstream Dify service timed out. Try again later or use streaming.")
	}

	if errors.Is(err, syscall.ECONNREFUSED) ||
		containsAny(message, "connection refused", "actively refused", "connectex") {
		return t(lang,
			"无法连接上游 Dify 服务，请检查 App 地址和服务状态。",
			"Could not connect to the upstream Dify service. Check the App address and service status.")
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) ||
		containsAny(message, "no such host", "server misbehaving", "name resolution", "resolve dify host", "dns") {
		return t(lang,
			"无法解析上游 Dify 服务地址，请检查 App 地址。",
			"Could not resolve the upstream Dify service address. Check the App address.")
	}

	var de *dify.DifyError
	if errors.As(err, &de) {
		switch {
		case de.Status == 200:
			return t(lang,
				"上游 Dify 工作流执行失败，请检查 App 日志。",
				"The upstream Dify workflow failed. Check the App logs.")
		case de.Status >= 400 && de.Status < 500:
			return t(lang,
				"上游 Dify 服务拒绝了请求，请检查 App 配置和输入。",
				"The upstream Dify service rejected the request. Check the App configuration and input.")
		case de.Status >= 500:
			return t(lang,
				"上游 Dify 服务暂时不可用，请稍后重试。",
				"The upstream Dify service is temporarily unavailable. Try again later.")
		}
	}

	if containsAny(message, "workflow failed", "workflow_finished", "status=failed", "status failed") {
		return t(lang,
			"上游 Dify 工作流执行失败，请检查 App 日志。",
			"The upstream Dify workflow failed. Check the App logs.")
	}

	var netErr net.Error
	if errors.As(err, &netErr) || containsAny(message,
		"http request", "dial tcp", "dial udp", "connection", "network", "transport", "tls", "eof") {
		return t(lang,
			"与上游 Dify 服务通信失败，请稍后重试。",
			"Communication with the upstream Dify service failed. Try again later.")
	}

	return t(lang,
		"上游 Dify 服务返回错误，请稍后重试。",
		"The upstream Dify service returned an error. Try again later.")
}

func containsAny(s string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// publicDifyErrorCode retains only known stable identifiers. Dify owns this
// field too, and HTTP-200 failures sometimes copy free-form error text into it,
// so syntax validation alone cannot distinguish a short secret from a code.
func publicDifyErrorCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "invalid_param", "app_unavailable", "server_overloaded", "timeout",
		"internal", "task_not_found", "invalid_request", "bad_request",
		"unauthorized", "forbidden", "not_found", "rate_limit_exceeded", "quota_exceeded":
		return code
	}
	return "upstream_error"
}

// sanitizePublicRequestLogs returns copies suitable for user-facing log APIs.
// It is intentionally separate from Store queries so future /api/me/all-logs
// endpoints can reuse the same display boundary without weakening admin views.
func sanitizePublicRequestLogs(logs []*db.RequestLog, lang string) []*db.RequestLog {
	out := make([]*db.RequestLog, 0, len(logs))
	for _, entry := range logs {
		if entry == nil {
			continue
		}
		publicEntry := *entry
		if publicEntry.ErrorDetail != "" {
			publicEntry.ErrorDetail = sanitizePublicUpstreamError(nil, publicEntry.ErrorDetail, lang)
		}
		out = append(out, &publicEntry)
	}
	return out
}

// sanitizePublicAdminRequestLogs is the equivalent adapter for the joined log
// shape used by the future level-5 user view. Admin handlers must not call it.
func sanitizePublicAdminRequestLogs(logs []*db.AdminRequestLog, lang string) []*db.AdminRequestLog {
	out := make([]*db.AdminRequestLog, 0, len(logs))
	for _, entry := range logs {
		if entry == nil {
			continue
		}
		publicEntry := *entry
		if publicEntry.ErrorDetail != "" {
			publicEntry.ErrorDetail = sanitizePublicUpstreamError(nil, publicEntry.ErrorDetail, lang)
		}
		out = append(out, &publicEntry)
	}
	return out
}
