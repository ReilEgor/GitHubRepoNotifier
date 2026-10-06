package model

// Subscriber is a confirmed subscription as seen by the tracking service.
type Subscriber struct {
	Email string
	Token string
}
