package contracts

// Queue names for events published by tracking.
const (
	QueueRepositoryTagUpdated = "repository.tag.updated"
)

// TagUpdatedEvent is published by tracking when a new release tag is detected.
// Consumed by subscription to refresh its copy of the last seen tag.
type TagUpdatedEvent struct {
	FullName  string `json:"full_name"`
	Tag       string `json:"tag"`
	RequestID string `json:"request_id,omitempty"`
}
