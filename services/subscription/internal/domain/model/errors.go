package model

import "errors"

// Domain errors of the subscription service.
var (
	ErrUserNotFound            = errors.New("user not found")
	ErrInvalidToken            = errors.New("invalid or expired token")
	ErrRepositoryNotFound      = errors.New("repository not found")
	ErrRepositoryHasNoReleases = errors.New("repository has no releases")
	ErrServiceUnavailable      = errors.New("external service is temporarily unavailable")
	ErrEmailRequired           = errors.New("email is required")
	ErrInvalidEmailFormat      = errors.New("invalid email format")
	ErrInvalidRepoFormat       = errors.New("invalid repository format (expected 'owner/repo')")
)
