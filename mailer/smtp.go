package mailer

import (
	"crypto/tls"
	"fmt"
	"net/smtp"

	"dify2api/config"
)

// sendSMTP delivers a single email via the configured SMTP server.
// It selects TLS mode based on cfg.TLS (starttls / implicit / auto).
func sendSMTP(cfg config.SMTPConfig, subject string, body string) error {
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

	if useImplicit {
		tlsCfg := &tls.Config{ServerName: cfg.Host}
		conn, err := tls.Dial("tcp", addr, tlsCfg)
		if err != nil {
			return fmt.Errorf("TLS dial: %w", err)
		}
		client, err := smtp.NewClient(conn, hostForAuth)
		if err != nil {
			return fmt.Errorf("SMTP client: %w", err)
		}
		defer client.Close()
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth: %w", err)
		}
		if err := client.Mail(from); err != nil {
			return fmt.Errorf("SMTP MAIL FROM: %w", err)
		}
		for _, t := range to {
			if err := client.Rcpt(t); err != nil {
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

	// starttls mode (or auto-detect non-465): Go's smtp.SendMail automatically
	// tries STARTTLS when the server advertises it.
	return smtp.SendMail(addr, auth, from, to, []byte(msg))
}
