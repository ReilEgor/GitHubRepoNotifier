package grpc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"testing"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/grpc/trackingv1"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/infrastructure/clients/github"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/repository/memory"
	trackinggrpc "github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/transport/grpc"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/usecase"
)

const testKey = "test-key"

func newClient(t *testing.T) pb.TrackingServiceClient {
	t.Helper()

	uc := usecase.NewRepositoryUseCase(memory.NewRepositoryRepository(), github.NewStaticClient())
	g := trackinggrpc.NewGRPCServer(testKey, trackinggrpc.NewServer(uc))
	lis := bufconn.Listen(1 << 20)
	go func() {
		if err := g.Serve(lis); err != nil {
			slog.Debug("test server stopped", slog.String("error", err.Error()))
		}
	}()
	t.Cleanup(g.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			slog.Debug("close test connection", slog.String("error", err.Error()))
		}
	})
	return pb.NewTrackingServiceClient(conn)
}

func TestServer_GetOrCreateRepository(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		repoName string
		wantCode codes.Code
	}{
		{name: "success - existing repository", key: testKey, repoName: github.StaticExisting, wantCode: codes.OK},
		{name: "error - repository not found", key: testKey, repoName: github.StaticNotFound, wantCode: codes.NotFound},
		{
			name:     "error - no releases",
			key:      testKey,
			repoName: github.StaticNoReleases,
			wantCode: codes.FailedPrecondition,
		},
		{
			name:     "error - github unavailable",
			key:      testKey,
			repoName: github.StaticUnavailable,
			wantCode: codes.Unavailable,
		},
		{
			name:     "error - rate limit",
			key:      testKey,
			repoName: github.StaticRateLimited,
			wantCode: codes.ResourceExhausted,
		},
		{name: "error - timeout", key: testKey, repoName: github.StaticTimeout, wantCode: codes.DeadlineExceeded},
		{name: "error - invalid name", key: testKey, repoName: "no-slash", wantCode: codes.InvalidArgument},
		{name: "error - wrong api key", key: "wrong", repoName: github.StaticExisting, wantCode: codes.Unauthenticated},
		{name: "error - missing api key", key: "", repoName: github.StaticExisting, wantCode: codes.Unauthenticated},
	}
	client := newClient(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.key != "" {
				ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", tt.key, "x-request-id", "req-1")
			}

			resp, err := client.GetOrCreateRepository(ctx, &pb.GetOrCreateRepositoryRequest{FullName: tt.repoName})

			assert.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode == codes.OK {
				assert.Equal(t, tt.repoName, resp.GetFullName())
				assert.Equal(t, "v1.0.0", resp.GetLastSeenTag())
				assert.NotZero(t, resp.GetId())
			}
		})
	}
}

func TestServer_logsEveryCallWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(ctxlog.NewHandler(slog.NewJSONHandler(&buf, nil))))
	t.Cleanup(func() { slog.SetDefault(prev) })
	client := newClient(t)

	calls := []struct {
		name, key, repo, wantMsg, wantCode string
	}{
		{"success - completed call", testKey, github.StaticExisting, "grpc call completed", "OK"},
		{"error - failed call", testKey, github.StaticNotFound, "grpc call failed", "NotFound"},
		{"error - rejected key is logged too", "wrong", github.StaticExisting, "grpc call failed", "Unauthenticated"},
	}
	for i, tt := range calls {
		t.Run(tt.name, func(t *testing.T) {
			id := fmt.Sprintf("req-%d", i)
			ctx := metadata.AppendToOutgoingContext(context.Background(), "x-api-key", tt.key, "x-request-id", id)
			buf.Reset()

			_, callErr := client.GetOrCreateRepository(ctx, &pb.GetOrCreateRepositoryRequest{FullName: tt.repo})
			assert.Equal(t, tt.wantCode, status.Code(callErr).String())

			var rec map[string]any
			for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte{10}) {
				var r map[string]any
				require.NoError(t, json.Unmarshal(line, &r))
				if r["msg"] == "grpc call completed" || r["msg"] == "grpc call failed" {
					require.Nil(t, rec, "exactly one call record per call")
					rec = r
				}
			}
			require.NotNil(t, rec, "the call is logged")
			assert.Equal(t, tt.wantMsg, rec["msg"])
			assert.Equal(t, tt.wantCode, rec["code"])
			assert.Equal(t, id, rec["request_id"])
			assert.Contains(t, rec, "duration")
		})
	}
}
