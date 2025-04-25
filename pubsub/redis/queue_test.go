package redis

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mashiike/a2a"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisPubSub(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "a2ago",
		DB:       0,
	})
	defer rdb.Close()

	pubsub := New(rdb)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	taskID := uuid.New().String()
	event := a2a.StreamingEvent{
		StatusUpdated: &a2a.TaskStatusUpdateEvent{
			ID: taskID,
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
				Message: &a2a.Message{
					Role: a2a.MessageRoleAgent,
					Parts: []a2a.Part{
						a2a.TextPart("test message"),
					},
					Metadata: make(map[string]any),
				},
			},
			Final:    true,
			Metadata: make(map[string]any),
		},
	}

	eventCh, err := pubsub.Subscribe(ctx, taskID)
	require.NoError(t, err)

	go func() {
		time.Sleep(1 * time.Second)
		err := pubsub.Publish(ctx, event)
		require.NoError(t, err)
	}()

	receivedEvent := <-eventCh
	assert.Equal(t, event, receivedEvent)
}
