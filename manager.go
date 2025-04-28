package a2a

import (
	"context"
	"errors"
	"io"
)

type TaskManager interface {
	WriteArtifact(ctx context.Context, artifact Artifact, metadata map[string]any) error
	SetStatus(ctx context.Context, status TaskStatus, metadata map[string]any) error
	GetTask(ctx context.Context, historyLength *int) (*Task, error)
}

type baseTaskManager struct {
	taskID string
	h      *Handler
}

func (h *Handler) NewTaskManager(taskID string) TaskManager {
	return &baseTaskManager{
		taskID: taskID,
		h:      h,
	}
}

func (m *baseTaskManager) WriteArtifact(ctx context.Context, artifact Artifact, metadata map[string]any) error {
	if err := m.h.store().UpdateArtifact(ctx, m.taskID, artifact); err != nil {
		return err
	}
	var errs []error
	if queue := m.h.queue(); queue != nil {
		if err := queue.Publish(ctx, StreamingEvent{
			ArtifactUpdated: &TaskArtifactUpdateEvent{
				ID:       m.taskID,
				Artifact: artifact,
				Metadata: metadata,
			},
		}); err != nil {
			errs = append(errs, err)
		}
	}
	if err := m.notify(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (m *baseTaskManager) stateTransitionHistory() bool {
	return m.h.card.Capabilities.StateTransitionHistory != nil && *m.h.card.Capabilities.StateTransitionHistory
}

func (m *baseTaskManager) SetStatus(ctx context.Context, status TaskStatus, metadata map[string]any) error {
	if err := m.h.store().UpdateStatus(ctx, m.taskID, status); err != nil {
		return err
	}
	var errs []error
	if queue := m.h.queue(); queue != nil {
		if err := queue.Publish(ctx, StreamingEvent{
			StatusUpdated: &TaskStatusUpdateEvent{
				ID:       m.taskID,
				Status:   status,
				Final:    status.State.Final(),
				Metadata: metadata,
			},
		}); err != nil {
			errs = append(errs, err)
		}
	}
	if m.stateTransitionHistory() && status.Message != nil {
		if err := m.h.store().AppendHistory(ctx, m.taskID, *status.Message); err != nil {
			errs = append(errs, err)
		}
	}
	if err := m.notify(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (m *baseTaskManager) GetTask(ctx context.Context, historyLength *int) (*Task, error) {
	task, err := m.h.store().GetTask(ctx, m.taskID, historyLength)
	if err != nil {
		return nil, err
	}
	return task, nil
}

func (m *baseTaskManager) notify(ctx context.Context) error {
	store, ok := m.h.store().(PushNotificationStore)
	if !ok {
		return nil
	}
	cfg, err := store.GetTaskPushNotification(ctx, m.taskID)
	if err != nil {
		if errors.Is(err, ErrTaskPushNotificationNotConfigured) {
			return nil
		}
		return err
	}
	if cfg == nil {
		return nil
	}
	task, err := m.h.store().GetTask(ctx, m.taskID, nil)
	if err != nil {
		return err
	}
	req, err := m.h.opts.PushNotificationRequestBuilder(ctx, cfg, task)
	if err != nil {
		return err
	}
	resp, err := m.h.opts.PushNotificationHTTPClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	bs, err := io.ReadAll(resp.Body)
	if err != nil {
		bs = []byte("[unable to read response body]")
	}
	m.h.logger().WarnContext(ctx, "failed to send notification", "status_code", resp.StatusCode, "task_id", m.taskID, "response", string(bs))
	return nil
}
