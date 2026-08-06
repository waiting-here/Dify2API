package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"

	"dify2api/config"
)

// sendSMTP delivers a single email via the configured SMTP server.
// It selects TLS mode based on cfg.TLS (starttls / implicit / auto).
func sendSMTP(ctx context.Context, cfg config.SMTPConfig, subject string, body string) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	from := cfg.From
	to := []string{cfg.To}

	msg := "From: " + from + "\r\n" +
		"To: " + cfg.To + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		body

	hostForAuth := smtpHostForAuth(cfg.Host)
	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, hostForAuth)

	useImplicit := false
	switch cfg.TLS {
	case "implicit":
		useImplicit = true
	case "starttls":
		useImplicit = false
	default:
		// auto-detect
		useImplicit = (cfg.Port == 465)
	}

	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("SMTP dial: %w", err)
	}
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	tlsCfg := &tls.Config{ServerName: hostForAuth}
	if useImplicit {
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return fmt.Errorf("TLS handshake: %w", err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, hostForAuth)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("SMTP client: %w", err)
	}
	defer client.Close()
	if !useImplicit {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsCfg); err != nil {
				return fmt.Errorf("SMTP STARTTLS: %w", err)
			}
		}
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("SMTP auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM: %w", err)
	}
	for _, recipient := range to {
		if err := client.Rcpt(recipient); err != nil {
			return fmt.Errorf("SMTP RCPT TO: %w", err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("SMTP write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP close: %w", err)
	}
	return client.Quit()
}
