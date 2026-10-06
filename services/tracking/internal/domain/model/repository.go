// Package model holds the entities of the tracking service.
package model

import "time"

// Repository is a GitHub repository watched for new releases.
type Repository struct {
	ID          int64
	FullName    string
	LastSeenTag string
	UpdatedAt   time.Time
}
