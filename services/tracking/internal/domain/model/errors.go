package model

import "errors"

// Domain errors of the tracking service.
var (
	ErrRepositoryNotFound = errors.New("repository not found")
	ErrReleaseNotFound    = errors.New("no releases found for this repository")
	ErrRateLimitExceeded  = errors.New("github api rate limit exceeded")
	ErrGitHubUnavailable  = errors.New("github service is temporarily unavailable")
)
