// Package model holds the entities of the subscription service.
package model

import "time"

// User is an email address that receives notifications.
type User struct {
	ID        int64
	Email     string
	CreatedAt time.Time
}
