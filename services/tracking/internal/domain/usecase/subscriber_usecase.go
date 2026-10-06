// Package usecase declares the business operation contracts of the tracking service.
package usecase

import "context"

// SubscriberUseCase keeps the list of confirmed subscribers of each repository.
// Queue consumers call it instead of reaching for the repositories themselves,
// so the rule "a repository without subscribers is no longer tracked" lives in one place.
type SubscriberUseCase interface {
	// Add registers a confirmed subscriber, creating the repository if it is not tracked yet.
	Add(ctx context.Context, repoName, email, token string) error
	// Remove deletes a subscriber and stops tracking the repository once nobody is subscribed to it.
	Remove(ctx context.Context, repoName, email string) error
}
