package adapter_test

import (
	"context"
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

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/infrastructure/adapter"
)

type fakeServer struct {
	pb.UnimplementedTrackingServiceServer

	code codes.Code
	md   metadata.MD
}

func (f *fakeServer) GetOrCreateRepository(
	ctx context.Context,
	req *pb.GetOrCreateRepositoryRequest,
) (*pb.GetOrCreateRepositoryResponse, error) {
	f.md, _ = metadata.FromIncomingContext(ctx)
	if f.code != codes.OK {
		return nil, status.Error(f.code, "x")
	}
	return &pb.GetOrCreateRepositoryResponse{Id: 5, FullName: req.GetFullName(), LastSeenTag: "v2"}, nil
}

func newClient(t *testing.T, srv *fakeServer) *adapter.TrackingClient {
	t.Helper()
	g := grpc.NewServer()
	pb.RegisterTrackingServiceServer(g, srv)
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
	return adapter.NewTrackingClient(conn, "key-1")
}

func TestTrackingClient_GetOrCreateRepository(t *testing.T) {
	tests := []struct {
		name    string
		code    codes.Code
		wantErr error
	}{
		{name: "success - repository returned", code: codes.OK},
		{name: "error - not found", code: codes.NotFound, wantErr: model.ErrRepositoryNotFound},
		{name: "error - no releases", code: codes.FailedPrecondition, wantErr: model.ErrRepositoryHasNoReleases},
		{name: "error - unavailable", code: codes.Unavailable, wantErr: model.ErrServiceUnavailable},
		{name: "error - rate limit", code: codes.ResourceExhausted, wantErr: model.ErrServiceUnavailable},
		{name: "error - deadline", code: codes.DeadlineExceeded, wantErr: model.ErrServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := &fakeServer{code: tt.code}
			c := newClient(t, srv)
			ctx := ctxlog.WithRequestID(context.Background(), "req-7")

			got, err := c.GetOrCreateRepository(ctx, "golang/go")

			assert.Equal(t, []string{"key-1"}, srv.md.Get("x-api-key"))
			assert.Equal(t, []string{"req-7"}, srv.md.Get("x-request-id"))
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, model.TrackedRepository{ID: 5, FullName: "golang/go", LastSeenTag: "v2"}, got)
		})
	}

	internal := []codes.Code{codes.Internal, codes.Unauthenticated, codes.InvalidArgument}
	for _, code := range internal {
		t.Run("error - "+code.String()+" is an internal error", func(t *testing.T) {
			c := newClient(t, &fakeServer{code: code})

			_, err := c.GetOrCreateRepository(context.Background(), "golang/go")

			require.Error(t, err)
			for _, domain := range []error{model.ErrRepositoryNotFound, model.ErrRepositoryHasNoReleases, model.ErrServiceUnavailable} {
				assert.NotErrorIs(t, err, domain)
			}
		})
	}
}
