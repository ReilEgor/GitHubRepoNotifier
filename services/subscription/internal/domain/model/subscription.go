package model

import "time"

// Subscription links a user to a repository.
type Subscription struct {
	ID             int64
	UserID         int64
	Email          string
	RepositoryID   int64
	RepositoryName string
	LastSeenTag    *string
	Token          string
	Confirmed      bool
	CreatedAt      time.Time
}
