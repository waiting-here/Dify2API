package handler

import (
	"context"
	"strings"
	"testing"

	"dify2api/dify"
)

func TestRemoteContentOriginGate(t *testing.T) {
	gw := setupTestGateway(t)
	if err := gw.requireRemoteOrigin("https://example.com/path?q=1"); err != nil {
		t.Fatalf("allowlisted origin rejected: %v", err)
	}
	for _, raw := range []string{
		"https://sub.example.com/path",
		"http://example.com/path",
		"https://user:pass@example.com/path",
		"https://example.com/path#fragment",
		"http://127.0.0.1/internal",
	} {
		if err := gw.requireRemoteOrigin(raw); err == nil {
			t.Errorf("remote URL %q should be rejected", raw)
		}
	}
}

func TestNewDifyClient_PrivateOriginRequiresOperatorAllowlist(t *testing.T) {
	gw := setupTestGateway(t)
	policy, err := dify.NewEgressPolicy(nil)
	if err != nil {
		t.Fatal(err)
	}
	gw.difyPolicy = policy
	if _, err := gw.newDifyClient("http://127.0.0.1:8080", "key", 0); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("private origin error = %v", err)
	}
}

func TestDifyProbeSemaphoreHonorsCancellation(t *testing.T) {
	gw := setupTestGateway(t)
	gw.difyProbeSem = make(chan struct{}, 1)
	release, err := gw.acquireDifyProbe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gw.acquireDifyProbe(ctx); err == nil {
		t.Fatal("probe acquire should stop when request context is canceled")
	}
}
