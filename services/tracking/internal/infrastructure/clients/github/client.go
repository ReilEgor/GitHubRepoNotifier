// Package github calls the GitHub REST API and caches its responses.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
)

const (
	maxBodyBytes = 1 << 20

	breakerFailures = 3
	breakerOpenFor  = 30 * time.Second
	breakerProbes   = 3

	rateLimitHeader = "X-RateLimit-Remaining"
)

// Config tunes the GitHub client.
type Config struct {
	// BaseURL is the API root, https://api.github.com in production.
	BaseURL string
	// Token is optional and raises the GitHub rate limit.
	Token string
	// Timeout bounds one attempt.
	Timeout time.Duration
	// Attempts is the number of tries of one request, the first one included.
	Attempts int
	// Backoff is the pause before the second attempt; it doubles before each further one.
	Backoff time.Duration
}

// DefaultConfig returns the production settings: 3 attempts of 3 s with pauses of 200 ms and 400 ms.
func DefaultConfig() Config {
	return Config{
		BaseURL:  "https://api.github.com",
		Timeout:  3 * time.Second,
		Attempts: 3,
		Backoff:  200 * time.Millisecond,
	}
}

type response struct {
	status    int
	remaining string
	body      []byte
}

// Client reads repositories and releases from GitHub. Only safe requests (HEAD and GET) are made, and only
// a timeout, a connection error or a 5xx answer is retried. The circuit breaker wraps the whole series of
// attempts, so one failed series counts as one failure.
type Client struct {
	http    *http.Client
	cfg     Config
	breaker *gobreaker.CircuitBreaker[response]
}

// NewClient returns a Client. A zero field of cfg takes its default.
func NewClient(cfg Config) *Client {
	def := DefaultConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = def.BaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = def.Timeout
	}
	if cfg.Attempts <= 0 {
		cfg.Attempts = def.Attempts
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = def.Backoff
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	breaker := gobreaker.NewCircuitBreaker[response](gobreaker.Settings{
		Name:        "github",
		MaxRequests: breakerProbes,
		Timeout:     breakerOpenFor,
		ReadyToTrip: func(c gobreaker.Counts) bool { return c.ConsecutiveFailures >= breakerFailures },
		// Only an exhausted series is a failure: a 404 or a rate limit answer proves GitHub is up.
		IsSuccessful: func(err error) bool { return err == nil || !errors.Is(err, model.ErrGitHubUnavailable) },
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Warn("circuit breaker state changed",
				slog.String("breaker", name), slog.String("from", from.String()), slog.String("to", to.String()))
		},
	})
	return &Client{http: &http.Client{}, cfg: cfg, breaker: breaker}
}

// CheckRepository returns model.ErrRepositoryNotFound when the repository does not exist.
func (c *Client) CheckRepository(ctx context.Context, fullName string) error {
	const op = "Client.CheckRepository"

	resp, err := c.request(ctx, http.MethodHead, "/repos/"+fullName)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	switch resp.status {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return model.ErrRepositoryNotFound
	default:
		return fmt.Errorf("%s: %w", op, statusError(resp))
	}
}

// LatestRelease returns model.ErrReleaseNotFound when the repository has no release.
func (c *Client) LatestRelease(ctx context.Context, fullName string) (model.ReleaseInfo, error) {
	const op = "Client.LatestRelease"

	resp, err := c.request(ctx, http.MethodGet, "/repos/"+fullName+"/releases/latest")
	if err != nil {
		return model.ReleaseInfo{}, fmt.Errorf("%s: %w", op, err)
	}
	switch resp.status {
	case http.StatusOK:
		var body struct {
			TagName string `json:"tag_name"`
		}
		if err := json.Unmarshal(resp.body, &body); err != nil {
			return model.ReleaseInfo{}, fmt.Errorf("%s: decode release: %w", op, err)
		}
		if body.TagName == "" {
			return model.ReleaseInfo{}, model.ErrReleaseNotFound
		}
		return model.ReleaseInfo{TagName: body.TagName}, nil
	case http.StatusNotFound:
		return model.ReleaseInfo{}, model.ErrReleaseNotFound
	default:
		return model.ReleaseInfo{}, fmt.Errorf("%s: %w", op, statusError(resp))
	}
}

// statusError classifies a final answer that is neither success nor "not found".
// A rate limit is 429, or 403 with no requests remaining; any other 4xx is a refusal that retrying cannot fix.
func statusError(r response) error {
	if r.status == http.StatusTooManyRequests || (r.status == http.StatusForbidden && r.remaining == "0") {
		return model.ErrRateLimitExceeded
	}
	return fmt.Errorf("github answered with unexpected status %d", r.status)
}

// request runs one series of attempts through the circuit breaker.
func (c *Client) request(ctx context.Context, method, path string) (response, error) {
	resp, err := c.breaker.Execute(func() (response, error) { return c.series(ctx, method, path) })
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return response{}, fmt.Errorf("circuit breaker: %w", model.ErrGitHubUnavailable)
	}
	if err != nil {
		return response{}, fmt.Errorf("%s %s: %w", method, path, err)
	}
	return resp, nil
}

func (c *Client) series(ctx context.Context, method, path string) (response, error) {
	var last error
	for attempt := range c.cfg.Attempts {
		if attempt > 0 {
			if err := sleep(ctx, c.cfg.Backoff<<(attempt-1)); err != nil {
				return response{}, err
			}
		}
		resp, err := c.attempt(ctx, method, path)
		switch {
		case ctx.Err() != nil:
			// The caller gave up; retrying would only waste GitHub's rate limit.
			return response{}, fmt.Errorf("request canceled: %w", ctx.Err())
		case err != nil:
			last = err
		case resp.status >= http.StatusInternalServerError:
			last = fmt.Errorf("github answered with status %d", resp.status)
		default:
			return resp, nil
		}
		slog.WarnContext(ctx, "github attempt failed",
			slog.Int("attempt", attempt+1), slog.String("path", path), slog.String("error", last.Error()))
	}
	return response{}, fmt.Errorf(
		"all %d attempts failed, last: %v: %w",
		c.cfg.Attempts,
		last,
		model.ErrGitHubUnavailable,
	)
}

func (c *Client) attempt(ctx context.Context, method, path string) (response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, nil)
	if err != nil {
		return response{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "reponotifier-tracking")
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return response{}, fmt.Errorf("send request: %w", err)
	}
	defer func() {
		if closeErr := res.Body.Close(); closeErr != nil {
			slog.DebugContext(ctx, "close github response body", slog.String("error", closeErr.Error()))
		}
	}()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxBodyBytes))
	if err != nil {
		return response{}, fmt.Errorf("read response: %w", err)
	}
	return response{status: res.StatusCode, remaining: res.Header.Get(rateLimitHeader), body: body}, nil
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait before retry: %w", ctx.Err())
	case <-t.C:
		return nil
	}
}
