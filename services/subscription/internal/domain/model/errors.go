package model

import "errors"

// Domain errors of the subscription service.
var (
	ErrUserNotFound       = errors.New("user not found")
	ErrInvalidToken       = errors.New("invalid or expired token")
	ErrRepositoryNotFound = errors.New("repository not found")
	ErrServiceUnavailable = errors.New("external service is temporarily unavailable")
)
