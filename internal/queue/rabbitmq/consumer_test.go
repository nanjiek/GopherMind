package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"gophermind/internal/core/model"
	"testing"
	"time"

	"github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"

	postgresrepo "gophermind/internal/repo/postgres"
	"gophermind/pkg/contracts/events"
)

type fakeCache struct {
	seen map[string]bool
}

func (f *fakeCache) GetSummary(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (f *fakeCache) SetSummary(_ context.Context, _, _, _ string, _ time.Duration) error {
	return nil
}
func (f *fakeCache) AppendStreamChunk(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}
func (f *fakeCache) GetStreamChunks(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (f *fakeCache) IsIdempotent(_ context.Context, _ string, messageID string) (bool, error) {
	return f.seen[messageID], nil
}
func (f *fakeCache) MarkIdempotent(_ context.Context, _ string, messageID string, _ time.Duration) error {
	f.seen[messageID] = true
	return nil
}
func (f *fakeCache) MarkDegraded(_ error) {}
func (f *fakeCache) IsDegraded() bool     { return false }

type fakeProducer struct {
	results    int
	retries    int
	dlqs       int
	failResult bool
	failRetry  bool
	failDLQ    bool
}

func (f *fakeProducer) PublishTask(_ context.Context, _ events.TaskMessage) error { return nil }
func (f *fakeProducer) PublishResult(_ context.Context, _ events.ResultMessage) error {
	if f.failResult {
		return errors.New("result publish failed")
	}
	f.results++
	return nil
}
func (f *fakeProducer) PublishRetryTask(_ context.Context, _ events.TaskMessage) error {
	if f.failRetry {
		return errors.New("retry publish failed")
	}
	f.retries++
	return nil
}
func (f *fakeProducer) PublishDLQTask(_ context.Context, _ events.TaskMessage) error {
	if f.failDLQ {
		return errors.New("dlq publish failed")
	}
	f.dlqs++
	return nil
}

type fakeInboxRepo struct {
	seen map[string]string
}

func (f *fakeInboxRepo) BeginProcess(_ context.Context, _ string, messageID string, _ int) (bool, error) {
	_, exists := f.seen[messageID]
	if exists {
		return false, nil
	}
	f.seen[messageID] = postgresrepo.InboxStatusProcessing
	return true, nil
}
func (f *fakeInboxRepo) MarkSucceeded(_ context.Context, _ string, messageID string) error {
	f.seen[messageID] = postgresrepo.InboxStatusSucceeded
	return nil
}
func (f *fakeInboxRepo) MarkFailed(_ context.Context, _ string, messageID string, _ int, _ string) error {
	f.seen[messageID] = postgresrepo.InboxStatusFailed
	return nil
}
func (f *fakeInboxRepo) MarkDead(_ context.Context, _ string, messageID string, _ int, _ string) error {
	f.seen[messageID] = postgresrepo.InboxStatusDead
	return nil
}

func TestConsumer_HandleDelivery_Idempotent(t *testing.T) {
	cache := &fakeCache{seen: map[string]bool{}}
	producer := &fakeProducer{}
	inboxRepo := &fakeInboxRepo{seen: map[string]string{}}
	c := &Consumer{
		cache:     cache,
		producer:  producer,
		inboxRepo: inboxRepo,
		maxRetry:  3,
	}

	task := events.TaskMessage{
		JobID:          "j1",
		RequestID:      "r1",
		IdempotencyKey: "r1",
	}
	raw, _ := json.Marshal(task)

	err := c.handleDelivery(context.Background(), amqp091.Delivery{Body: raw})
	require.NoError(t, err)
	require.Equal(t, 1, producer.results)

	err = c.handleDelivery(context.Background(), amqp091.Delivery{Body: raw})
	require.NoError(t, err)
	require.Equal(t, 1, producer.results)
}

func TestConsumer_HandleDelivery_InvalidJSONToDLQ(t *testing.T) {
	cache := &fakeCache{seen: map[string]bool{}}
	producer := &fakeProducer{}
	inboxRepo := &fakeInboxRepo{seen: map[string]string{}}
	c := &Consumer{
		cache:     cache,
		producer:  producer,
		inboxRepo: inboxRepo,
		maxRetry:  3,
	}

	err := c.handleDelivery(context.Background(), amqp091.Delivery{Body: []byte("{invalid-json")})
	require.NoError(t, err)
	require.Equal(t, 1, producer.dlqs)
}

func TestConsumer_HandleDelivery_RetryPath(t *testing.T) {
	cache := &fakeCache{seen: map[string]bool{}}
	producer := &fakeProducer{failResult: true}
	inboxRepo := &fakeInboxRepo{seen: map[string]string{}}
	c := &Consumer{
		cache:     cache,
		producer:  producer,
		inboxRepo: inboxRepo,
		maxRetry:  3,
	}

	task := events.TaskMessage{
		JobID:          "j-retry",
		RequestID:      "r-retry",
		IdempotencyKey: "r-retry",
	}
	raw, _ := json.Marshal(task)

	err := c.handleDelivery(context.Background(), amqp091.Delivery{Body: raw})
	require.NoError(t, err)
	require.Equal(t, 1, producer.retries)
	require.Equal(t, postgresrepo.InboxStatusFailed, inboxRepo.seen["r-retry"])
}

func (f *fakeCache) GetWindow(_ context.Context, _, _ string) ([]model.Message, bool, error) {
	return nil, false, nil
}
func (f *fakeCache) SetWindow(_ context.Context, _, _ string, _ []model.Message, _ time.Duration) error {
	return nil
}
