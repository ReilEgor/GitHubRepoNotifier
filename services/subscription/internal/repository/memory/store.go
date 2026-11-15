// Package memory implements the repositories of the subscription service in process memory.
//
// It exists for the prototype stage. A transaction works on a copy of the data that replaces the committed
// data only when the transaction function succeeds. Every call, inside a transaction or not, is serialized
// by one mutex, so a write made with the outer context while a transaction is open waits for it.
package memory

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	sharedModel "github.com/ReilEgor/GitHubRepoNotifier/shared/domain/model"

	"github.com/ReilEgor/GitHubRepoNotifier/services/subscription/internal/domain/model"
)

type txKey struct{}

type repositoryRow struct {
	id       int64
	fullName string
	tag      string
}

type subscriptionRow struct {
	id           int64
	userID       int64
	repositoryID int64
	token        string
	confirmed    bool
	createdAt    time.Time
}

type state struct {
	users         []model.User
	repositories  []repositoryRow
	subscriptions []subscriptionRow
	sagas         []model.SubscriptionSaga
	outbox        []sharedModel.OutboxMessage
	lastID        map[string]int64
}

func newState() *state {
	return &state{lastID: map[string]int64{}}
}

func (s *state) nextID(table string) int64 {
	s.lastID[table]++
	return s.lastID[table]
}

func (s *state) clone() *state {
	c := &state{
		users:         append([]model.User(nil), s.users...),
		repositories:  append([]repositoryRow(nil), s.repositories...),
		subscriptions: append([]subscriptionRow(nil), s.subscriptions...),
		sagas:         append([]model.SubscriptionSaga(nil), s.sagas...),
		outbox:        append([]sharedModel.OutboxMessage(nil), s.outbox...),
		lastID:        make(map[string]int64, len(s.lastID)),
	}
	maps.Copy(c.lastID, s.lastID)
	return c
}

// Store holds the data of every in-memory repository and implements the Transactor.
type Store struct {
	mu    sync.Mutex
	state *state
}

// NewStore returns an empty Store.
func NewStore() *Store {
	return &Store{state: newState()}
}

// InTransaction reports whether ctx belongs to an open transaction of a Store.
func InTransaction(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(*state)
	return ok
}

// WithinTransaction runs fn on a copy of the data and keeps the copy only if fn returns nil.
// A nested call joins the transaction already carried by ctx.
func (s *Store) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if InTransaction(ctx) {
		return fn(ctx)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	staged := s.state.clone()
	if err := fn(context.WithValue(ctx, txKey{}, staged)); err != nil {
		return fmt.Errorf("transaction: %w", err)
	}
	s.state = staged
	return nil
}

// Outbox returns the messages waiting to be published.
func (s *Store) Outbox() []sharedModel.OutboxMessage {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]sharedModel.OutboxMessage(nil), s.state.outbox...)
}

// Sagas returns all sagas.
func (s *Store) Sagas() []model.SubscriptionSaga {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([]model.SubscriptionSaga(nil), s.state.sagas...)
}

// run executes fn on the data of the transaction in ctx, or on the committed data.
func (s *Store) run(ctx context.Context, fn func(st *state) error) error {
	if st, ok := ctx.Value(txKey{}).(*state); ok {
		return fn(st)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return fn(s.state)
}
