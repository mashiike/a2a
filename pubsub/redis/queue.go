package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mashiike/a2a"
	"github.com/redis/go-redis/v9"
)

type PubSub struct {
	rdb           redis.UniversalClient
	ChannelPrefix string
}

func New(rdb redis.UniversalClient) *PubSub {
	s := &PubSub{rdb: rdb, ChannelPrefix: "a2a:task:"}
	return s
}

var _ a2a.PubSub = (*PubSub)(nil)

func (ps *PubSub) Publish(ctx context.Context, event a2a.StreamingEvent) error {
	channel := ps.getChannelNameFromEvents(event)
	if channel == "" {
		return errors.New("event must have either StatusUpdated or ArtifactUpdated")
	}

	data, err := json.Marshal(&event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	if err := ps.rdb.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("failed to publish event: %w", err)
	}
	return nil
}

func (ps *PubSub) Subscribe(ctx context.Context, taskID string) (<-chan a2a.StreamingEvent, error) {
	channel := ps.getChannelName(taskID)
	sub := ps.rdb.Subscribe(ctx, channel)
	if err := sub.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping Redis: %w", err)
	}
	ch := make(chan a2a.StreamingEvent, 10)

	go func() {
		defer close(ch)
		defer func() {
			cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := sub.Unsubscribe(cctx, channel); err != nil {
				slog.Error("failed to unsubscribe", "component", "redis pubsub", "error", err)

			}
			if err := sub.Close(); err != nil {
				slog.Error("failed to receive message", "component", "redis pubsub", "error", err)
				return
			}
		}()
		slog.Debug("subscribing to channel", "component", "redis pubsub", "channel", channel)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			msg, err := sub.ReceiveMessage(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, redis.Nil) || errors.Is(err, context.DeadlineExceeded) {
					return
				}
				slog.Error("failed to receive message", "component", "redis pubsub", "error", err)
			}

			var event a2a.StreamingEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				slog.Warn("failed to unmarshal message", "component", "redis pubsub", "error", err)
				continue
			}

			select {
			case ch <- event:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (ps *PubSub) getChannelNameFromEvents(event a2a.StreamingEvent) string {
	if event.StatusUpdated != nil {
		return ps.getChannelName(event.StatusUpdated.ID)
	}
	if event.ArtifactUpdated != nil {
		return ps.getChannelName(event.ArtifactUpdated.ID)
	}
	return ""
}
func (ps *PubSub) getChannelName(taskID string) string {
	return fmt.Sprintf("%s%s", ps.ChannelPrefix, taskID)
}
