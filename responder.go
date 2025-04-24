package a2a

import (
	"context"
	"errors"
	"io"
)

type TaskResponder interface {
	WriteArtifact(ctx context.Context, artifact Artifact, metadata map[string]any) error
	SetStatus(ctx context.Context, status TaskStatus, metadata map[string]any) error
}

type baseTaskResponder struct {
	taskID string
	h      *Handler
}

func (h *Handler) NewTaskResponder(taskID string) TaskResponder {
	return &baseTaskResponder{
		taskID: taskID,
		h:      h,
	}
}

func (tr *baseTaskResponder) WriteArtifact(ctx context.Context, artifact Artifact, metadata map[string]any) error {
	if err := tr.h.store.UpdateArtifact(ctx, tr.taskID, artifact); err != nil {
		return err
	}
	var errs []error
	if tr.h.queue != nil {
		if err := tr.h.queue.Publish(ctx, StreamingEvent{
			ArtifactUpdated: &TaskArtifactUpdateEvent{
				ID:       tr.taskID,
				Artifact: artifact,
				Metadata: metadata,
			},
		}); err != nil {
			errs = append(errs, err)
		}
	}
	if err := tr.notify(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (tr *baseTaskResponder) stateTransitionHistory() bool {
	return tr.h.card.Capabilities.StateTransitionHistory != nil && *tr.h.card.Capabilities.StateTransitionHistory
}

func (tr *baseTaskResponder) SetStatus(ctx context.Context, status TaskStatus, metadata map[string]any) error {
	if err := tr.h.store.UpdateStatus(ctx, tr.taskID, status); err != nil {
		return err
	}
	var errs []error
	if tr.h.queue != nil {
		if err := tr.h.queue.Publish(ctx, StreamingEvent{
			StatusUpdated: &TaskStatusUpdateEvent{
				ID:       tr.taskID,
				Status:   status,
				Final:    status.State.Final(),
				Metadata: metadata,
			},
		}); err != nil {
			errs = append(errs, err)
		}
	}
	if tr.stateTransitionHistory() && status.Message != nil {
		if err := tr.h.store.AppendHistory(ctx, tr.taskID, *status.Message); err != nil {
			errs = append(errs, err)
		}
	}
	if err := tr.notify(ctx); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (tr *baseTaskResponder) notify(ctx context.Context) error {
	store, ok := tr.h.store.(PushNotificationStore)
	if !ok {
		return nil
	}
	cfg, err := store.GetTaskPushNotification(ctx, tr.taskID)
	if err != nil {
		if errors.Is(err, ErrTaskPushNotificationNotConfigured) {
			return nil
		}
		return err
	}
	if cfg == nil {
		return nil
	}
	task, err := tr.h.store.GetTask(ctx, tr.taskID, nil)
	if err != nil {
		return err
	}
	req, err := tr.h.notificationRequestBuilder(ctx, cfg, task)
	if err != nil {
		return err
	}
	resp, err := tr.h.notificationHTTPClient.Do(req)
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
	tr.h.logger.WarnContext(ctx, "failed to send notification", "status_code", resp.StatusCode, "task_id", tr.taskID, "response", string(bs))
	return nil
}
