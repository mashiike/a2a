package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// DefaultPushNotificationRequestBuilder is the default request builder for push notifications.
// It creates a JSON request with the task as the body and sets the content type to application/json.
// but this builder does not authonication.
func DefaultPushNotificationRequestBuilder(
	ctx context.Context,
	cfg *TaskPushNotificationConfig,
	task *Task,
) (*http.Request, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(task); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.PushNotificationConfig.URL, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}
