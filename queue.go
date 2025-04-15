package a2a

import (
	"context"
	"errors"
	"sync"

	"github.com/google/uuid"
)

type PubSub interface {
	Publish(ctx context.Context, event StreamingEvent) error
	Subscribe(ctx context.Context, taskID string) (<-chan StreamingEvent, error)
}

// ChannelPubSub is a simple in-memory Pub/Sub implementation using channels.
type ChannelPubSub struct {
	wg       sync.WaitGroup
	onceInit sync.Once

	subscriberMu sync.Mutex
	subscriber   map[string]map[string]chan StreamingEvent
	cancelFuncs  map[string]context.CancelFunc
}

func (ps *ChannelPubSub) init() {
	ps.onceInit.Do(func() {
		ps.subscriber = make(map[string]map[string]chan StreamingEvent)
		ps.cancelFuncs = make(map[string]context.CancelFunc)
	})
}

func (ps *ChannelPubSub) Publish(ctx context.Context, event StreamingEvent) error {
	ps.init()
	ps.subscriberMu.Lock()
	defer ps.subscriberMu.Unlock()

	var taskID string
	if event.StatusUpdated != nil {
		taskID = event.StatusUpdated.ID
	} else if event.ArtifactUpdated != nil {
		taskID = event.ArtifactUpdated.ID
	} else {
		return errors.New("event must have either StatusUpdated or ArtifactUpdated")
	}
	subscribers, ok := ps.subscriber[taskID]
	if !ok {
		return nil
	}
	for _, subscriber := range subscribers {
		subscriber <- event
	}
	return nil
}

func (ps *ChannelPubSub) Subscribe(ctx context.Context, taskID string) (<-chan StreamingEvent, error) {
	ps.init()
	ps.subscriberMu.Lock()
	defer ps.subscriberMu.Unlock()

	outputCh := make(chan StreamingEvent, 10)
	internalCh := make(chan StreamingEvent, 10)
	subscriberID := uuid.New().String()
	ctx, cancel := context.WithCancel(ctx)
	ps.wg.Add(1)
	ps.cancelFuncs[subscriberID] = cancel
	if _, ok := ps.subscriber[taskID]; !ok {
		ps.subscriber[taskID] = make(map[string]chan StreamingEvent)
	}
	ps.subscriber[taskID][subscriberID] = internalCh
	go func() {
		defer func() {
			ps.subscriberMu.Lock()
			defer ps.subscriberMu.Unlock()
			cancel()
			delete(ps.cancelFuncs, subscriberID)
			if _, ok := ps.subscriber[taskID]; ok {
				if ch, ok := ps.subscriber[taskID][subscriberID]; ok {
					close(ch)
				}
				delete(ps.subscriber[taskID], subscriberID)
			}
			close(outputCh)
			ps.wg.Done()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-internalCh:
				outputCh <- event
				if event.StatusUpdated != nil && event.StatusUpdated.Final {
					return
				}
			}
		}
	}()
	return outputCh, nil
}

// io.Closer interface
func (ps *ChannelPubSub) Close() error {
	ps.subscriberMu.Lock()
	for _, cancel := range ps.cancelFuncs {
		cancel()
	}
	ps.subscriberMu.Unlock()
	ps.wg.Wait()
	return nil
}
