package handler

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"dify2api/dify"
)

func (g *Gateway) newDifyClient(userID int64, baseURL, apiKey string, timeout time.Duration) (*dify.Client, error) {
	if _, err := g.difyPolicy.ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	client := dify.NewClientWithOptions(baseURL, apiKey, dify.ClientOptions{
		Timeout:          timeout,
		EgressPolicy:     g.difyPolicy,
		MaxResponseBytes: int64(g.Config.DifyMaxResponseMB) << 20,
		SSEBufferSize:    g.Config.SSEBufferMB << 20,
		UserID:           userID,
	})
	return client, nil
}

func (g *Gateway) acquireDifyProbe(ctx context.Context) (func(), error) {
	select {
	case g.difyProbeSem <- struct{}{}:
		return func() { <-g.difyProbeSem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// validateRemoteContent confines URLs that a remote Dify workflow will fetch
// to exact origins chosen by the deployment operator. The gateway cannot pin
// DNS or redirects performed inside somebody else's Dify installation, so an
// empty allowlist intentionally disables website-summary and remote images.
func (g *Gateway) validateRemoteContent(service string, inputs map[string]string, images []string) error {
	if service == "website-summary" {
		if err := g.requireRemoteOrigin(inputs["request_url"]); err != nil {
			return fmt.Errorf("request_url: %w", err)
		}
	}
	for i, image := range images {
		if strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
			if err := g.requireRemoteOrigin(image); err != nil {
				return fmt.Errorf("image %d: %w", i+1, err)
			}
		}
	}
	return nil
}

func (g *Gateway) requireRemoteOrigin(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("must be an http(s) URL without credentials or fragment")
	}
	origin := canonicalOrigin(u)
	if _, ok := g.remoteContentOrigins[origin]; !ok {
		return fmt.Errorf("origin %q is not allowed by REMOTE_CONTENT_ORIGIN_ALLOWLIST", origin)
	}
	return nil
}

func (g *Gateway) stopDifyWorkflow(client *dify.Client, taskID, user string) {
	if taskID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.StopWorkflowContext(ctx, taskID, user); err != nil {
		log.Printf("%s", boundedStopWorkflowDiagnostic(taskID, err))
	}
}

const statusClientClosedRequest = 499

func canonicalOrigin(u *url.URL) string {
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return strings.ToLower(u.Scheme) + "://" + host
}
