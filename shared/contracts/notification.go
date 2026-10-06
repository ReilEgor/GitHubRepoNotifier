package contracts

// Queue names for notification-related message flows.
const (
	QueueNotifications           = "notifications"
	QueueConfirmations           = "confirmations"
	QueueSagaConfirmationResults = "saga.confirmation.results"
)

// SendNotificationCommand is published by tracking when a new release is found.
// Consumed by notification to email the subscriber.
type SendNotificationCommand struct {
	Email     string `json:"email"`
	RepoName  string `json:"repo_name"`
	Tag       string `json:"tag"`
	Token     string `json:"token"`
	RequestID string `json:"request_id,omitempty"`
}

// SendConfirmationCommand is published by subscription (via saga) when a user subscribes.
// Consumed by notification to send the confirmation email.
type SendConfirmationCommand struct {
	Email          string `json:"email"`
	RepoName       string `json:"repo_name"`
	Token          string `json:"token"`
	SagaID         int64  `json:"saga_id"`
	SubscriptionID int64  `json:"subscription_id"`
	RequestID      string `json:"request_id,omitempty"`
}

// ConfirmationResultEvent is published by notification after processing a confirmation.
// Consumed by subscription saga to advance or compensate the saga.
type ConfirmationResultEvent struct {
	SagaID         int64  `json:"saga_id"`
	SubscriptionID int64  `json:"subscription_id"`
	Success        bool   `json:"success"`
	Email          string `json:"email"`
	RepoName       string `json:"repo_name"`
	Token          string `json:"token"`
	RequestID      string `json:"request_id,omitempty"`
}
