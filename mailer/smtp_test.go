package mailer

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"dify2api/config"
)

func TestSendSMTP_ContextCancellationClosesBlockedConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	release := make(chan struct{})
	accepted := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			close(accepted)
			return
		}
		close(accepted)
		<-release
		_ = conn.Close()
	}()
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	err = sendSMTP(ctx, config.SMTPConfig{
		Host: host, Port: port, User: "u", Pass: "p", From: "from@example.com", To: "to@example.com", TLS: "starttls",
	}, "subject", "body")
	close(release)
	<-accepted
	if err == nil {
		t.Fatal("sendSMTP unexpectedly succeeded against a stalled server")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		// net/smtp may wrap the connection close rather than the context; the
		// bounded return is the lifecycle guarantee under test.
		t.Logf("sendSMTP returned cancellation-induced network error: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("sendSMTP ignored context cancellation for %v", elapsed)
	}
}
