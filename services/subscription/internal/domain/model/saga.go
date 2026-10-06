package model

import "time"

// SagaStatus is the state of the confirmation email saga.
type SagaStatus string

// States of the confirmation email saga.
const (
	SagaStatusStarted     SagaStatus = "STARTED"
	SagaStatusCompleted   SagaStatus = "COMPLETED"
	SagaStatusCompensated SagaStatus = "COMPENSATED"
)

// SagaStep is the step the saga is currently at.
type SagaStep string

// Steps of the confirmation email saga.
const (
	SagaStepSendConfirmation     SagaStep = "SEND_CONFIRMATION"
	SagaStepActivateSubscription SagaStep = "ACTIVATE_SUBSCRIPTION"
)

// SubscriptionSaga tracks the delivery of the confirmation email for a subscription.
type SubscriptionSaga struct {
	ID             int64
	SubscriptionID int64
	Status         SagaStatus
	CurrentStep    SagaStep
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
