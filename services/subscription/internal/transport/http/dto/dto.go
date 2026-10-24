// Package dto holds the request and response bodies of the HTTP API.
package dto

import (
	"time"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

// SubscribeRequest is the body of POST /api/v1/subscribe.
type SubscribeRequest struct {
	Email      string `json:"email"`
	Repository string `json:"repository"`
}

// MessageResponse is the body of a successful action.
type MessageResponse struct {
	Message string `json:"message"`
}

// SubscriptionResponse is one entry of the subscription list. It never carries the token.
type SubscriptionResponse struct {
	ID             int64     `json:"id"`
	Email          string    `json:"email"`
	RepositoryName string    `json:"repository_name"`
	Confirmed      bool      `json:"confirmed"`
	LastSeenTag    *string   `json:"last_seen_tag"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListResponse is the body of GET /api/v1/subscriptions.
type ListResponse struct {
	Subscriptions []SubscriptionResponse `json:"subscriptions"`
	Total         int                    `json:"total"`
}

// NewListResponse converts subscriptions to the response body.
func NewListResponse(subs []model.Subscription) ListResponse {
	items := make([]SubscriptionResponse, 0, len(subs))
	for _, s := range subs {
		items = append(items, SubscriptionResponse{
			ID:             s.ID,
			Email:          s.Email,
			RepositoryName: s.RepositoryName,
			Confirmed:      s.Confirmed,
			LastSeenTag:    s.LastSeenTag,
			CreatedAt:      s.CreatedAt.UTC(),
		})
	}
	return ListResponse{Subscriptions: items, Total: len(items)}
}

// ErrorResponse is the body of a failed request with one error.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ErrorsResponse is the body of a request that failed validation with several errors.
type ErrorsResponse struct {
	Errors []string `json:"errors"`
}
