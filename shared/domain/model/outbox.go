// Package model holds the entities shared by every service.
package model

// OutboxStatus is the delivery state of an outbox message.
type OutboxStatus string

// Delivery states of an outbox message. A published message is deleted, so it has no state.
const (
	OutboxStatusPending OutboxStatus = "PENDING"
	OutboxStatusFailed  OutboxStatus = "FAILED"
)

// OutboxMessage is a message waiting to be published to the broker.
type OutboxMessage struct {
	ID      int64
	Queue   string
	Payload []byte
}
