// Package adapter calls other services on behalf of the subscription use cases.
package adapter

import (
	"context"
	"fmt"

	"github.com/ReilEgor/GitHubRepoNotifier/shared/ctxlog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/ReilEgor/GitHubRepoNotifier/shared/infrastructure/grpc/trackingv1"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

// TrackingClient calls TrackingService and translates its status codes into domain errors.
type TrackingClient struct {
	client pb.TrackingServiceClient
	apiKey string
}

// NewTrackingClient returns a TrackingClient on conn.
func NewTrackingClient(conn grpc.ClientConnInterface, apiKey string) *TrackingClient {
	return &TrackingClient{client: pb.NewTrackingServiceClient(conn), apiKey: apiKey}
}

// GetOrCreateRepository asks tracking for the repository. The deadline of ctx bounds the call.
func (c *TrackingClient) GetOrCreateRepository(ctx context.Context, fullName string) (model.TrackedRepository, error) {
	const op = "TrackingClient.GetOrCreateRepository"

	md := metadata.Pairs("x-api-key", c.apiKey)
	if id := ctxlog.RequestID(ctx); id != "" {
		md.Set("x-request-id", id)
	}
	ctx = metadata.NewOutgoingContext(ctx, md)

	resp, err := c.client.GetOrCreateRepository(ctx, &pb.GetOrCreateRepositoryRequest{FullName: fullName})
	if err != nil {
		return model.TrackedRepository{}, fmt.Errorf("%s: %w", op, translate(err))
	}
	return model.TrackedRepository{
		ID:          resp.GetId(),
		FullName:    resp.GetFullName(),
		LastSeenTag: resp.GetLastSeenTag(),
	}, nil
}

// domainErrors maps the gRPC codes tracking uses for expected outcomes to domain errors.
var domainErrors = map[codes.Code]error{
	codes.NotFound:           model.ErrRepositoryNotFound,
	codes.FailedPrecondition: model.ErrRepositoryHasNoReleases,
	codes.Unavailable:        model.ErrServiceUnavailable,
	codes.ResourceExhausted:  model.ErrServiceUnavailable,
	codes.DeadlineExceeded:   model.ErrServiceUnavailable,
}

// translate maps a gRPC status to a domain error; any other failure stays an internal error.
func translate(err error) error {
	if domainErr, ok := domainErrors[status.Code(err)]; ok {
		return fmt.Errorf("%w: %s", domainErr, status.Code(err))
	}
	return fmt.Errorf("tracking call failed: %w", err)
}
