// Package grpc exposes TrackingService over gRPC.
package grpc

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"time"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/grpc/trackingv1"

	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/model"
	"github.com/ReilEgor/GitHubRepoNotifier/services/tracking/internal/domain/usecase"
)

const (
	metadataAPIKey    = "x-api-key"
	metadataRequestID = "x-request-id"
	stopTimeout       = 5 * time.Second
)

var repoNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)

// Server implements pb.TrackingServiceServer.
type Server struct {
	pb.UnimplementedTrackingServiceServer

	repositories usecase.RepositoryUseCase
}

// NewServer returns a Server.
func NewServer(repositories usecase.RepositoryUseCase) *Server {
	return &Server{repositories: repositories}
}

// GetOrCreateRepository returns the repository, registering it when it is new.
func (s *Server) GetOrCreateRepository(
	ctx context.Context,
	req *pb.GetOrCreateRepositoryRequest,
) (*pb.GetOrCreateRepositoryResponse, error) {
	if !repoNamePattern.MatchString(req.GetFullName()) {
		return nil, status.Error(codes.InvalidArgument, "invalid repository name")
	}
	repo, err := s.repositories.GetOrCreate(ctx, req.GetFullName())
	if err != nil {
		return nil, toStatus(err)
	}
	return &pb.GetOrCreateRepositoryResponse{Id: repo.ID, FullName: repo.FullName, LastSeenTag: repo.LastSeenTag}, nil
}

// toStatus translates a domain error into a gRPC status.
func toStatus(err error) error {
	switch {
	case errors.Is(err, model.ErrRepositoryNotFound):
		return status.Error(codes.NotFound, "repository not found")
	case errors.Is(err, model.ErrReleaseNotFound):
		return status.Error(codes.FailedPrecondition, "repository has no releases")
	case errors.Is(err, model.ErrRateLimitExceeded):
		return status.Error(codes.ResourceExhausted, "github rate limit exceeded")
	case errors.Is(err, model.ErrGitHubUnavailable):
		return status.Error(codes.Unavailable, "github is unavailable")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

// NewGRPCServer returns a grpc.Server that checks the API key and puts the request ID into the context.
func NewGRPCServer(apiKey string, srv *Server) *grpc.Server {
	g := grpc.NewServer(grpc.ChainUnaryInterceptor(requestContext, authenticate(apiKey), logErrors))
	pb.RegisterTrackingServiceServer(g, srv)
	return g
}

func requestContext(
	ctx context.Context,
	req any,
	_ *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (any, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get(metadataRequestID); len(ids) > 0 {
			ctx = ctxlog.WithRequestID(ctx, ids[0])
		}
	}
	return handler(ctx, req)
}

func authenticate(apiKey string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		keys := md.Get(metadataAPIKey)
		if len(keys) == 0 || subtle.ConstantTimeCompare([]byte(keys[0]), []byte(apiKey)) != 1 {
			return nil, status.Error(codes.Unauthenticated, "invalid or missing API key")
		}
		return handler(ctx, req)
	}
}

// logErrors is the single place where a failed call is logged.
func logErrors(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	resp, err := handler(ctx, req)
	if err != nil {
		level := slog.LevelWarn
		switch status.Code(err) {
		case codes.Internal, codes.Unavailable, codes.ResourceExhausted, codes.DeadlineExceeded:
			level = slog.LevelError
		case codes.OK, codes.Canceled, codes.Unknown, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
			codes.PermissionDenied, codes.FailedPrecondition, codes.Aborted, codes.OutOfRange, codes.Unimplemented,
			codes.DataLoss, codes.Unauthenticated:
		}
		slog.Log(
			ctx,
			level,
			"grpc call failed",
			slog.String("method", info.FullMethod),
			slog.String("error", err.Error()),
		)
	}
	return resp, err //nolint:wrapcheck // a gRPC status must reach the client unchanged.
}

// Run serves g on addr until ctx is canceled, then stops it gracefully.
func Run(ctx context.Context, addr string, g *grpc.Server) error {
	const op = "grpc.Run"

	lc := net.ListenConfig{}
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("%s: listen: %w", op, err)
	}

	errCh := make(chan error, 1)
	go func() { errCh <- g.Serve(lis) }()

	select {
	case err := <-errCh:
		return fmt.Errorf("%s: serve: %w", op, err)
	case <-ctx.Done():
	}

	stopped := make(chan struct{})
	go func() {
		g.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(stopTimeout):
		g.Stop()
	}
	return nil
}
