//go:build integration

// Package redistest starts a Redis container for integration tests.
package redistest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	image       = "redis:7-alpine"
	port        = "6379/tcp"
	startupWait = 60 * time.Second
	opTimeout   = 10 * time.Second
)

// Server is a Redis container shared by all tests of one package.
type Server struct {
	container testcontainers.Container
	addr      string
}

// Start runs the container; call it from TestMain and Stop it afterwards.
func Start(ctx context.Context) (*Server, error) {
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{port},
			WaitingFor:   wait.ForLog("Ready to accept connections").WithStartupTimeout(startupWait),
		},
		Started: true,
	})
	if err != nil {
		return nil, fmt.Errorf("start redis container: %w", err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		return nil, fmt.Errorf("get redis host: %w", err)
	}
	mapped, err := c.MappedPort(ctx, port)
	if err != nil {
		return nil, fmt.Errorf("get redis port: %w", err)
	}
	return &Server{container: c, addr: fmt.Sprintf("%s:%s", host, mapped.Port())}, nil
}

// Stop terminates the container.
func (s *Server) Stop() error {
	if err := testcontainers.TerminateContainer(s.container); err != nil {
		return fmt.Errorf("terminate redis container: %w", err)
	}
	return nil
}

// Addr returns host:port of the server.
func (s *Server) Addr() string { return s.addr }

// Flush removes every key, so a test starts from an empty Redis. It uses redis-cli inside the container
// to keep this package free of a client dependency.
func (s *Server) Flush(t testing.TB) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	code, _, err := s.container.Exec(ctx, []string{"redis-cli", "FLUSHALL"})
	if err != nil || code != 0 {
		t.Fatalf("flush redis: exit code %d, error %v", code, err)
	}
}
