package s3

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/google/uuid"
	"github.com/mashiike/a2a"
	"github.com/stretchr/testify/require"
)

const (
	testBucketName = "test-bucket"
)

func TestStore(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-west-2",
		Credentials: credentials.NewStaticCredentialsProvider("minio0admin", "minio0admin", ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String("http://localhost:9000")
		o.UsePathStyle = true
	})
	ctx := context.Background()
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(testBucketName),
	})
	var apiErr smithy.APIError
	if err != nil {
		if ok := errors.As(err, &apiErr); !ok || apiErr.ErrorCode() != "BucketAlreadyOwnedByYou" {
			require.FailNow(t, "failed to create test bucket:"+err.Error())
		}
	}
	store := NewWithClient(client, testBucketName)
	taskID := uuid.New().String()
	t.Run("UpsertTask on new", func(t *testing.T) {
		taskParams := a2a.TaskSendParams{
			ID:            taskID,
			HistoryLength: a2a.Ptr(10),
			Message: a2a.Message{
				Role: a2a.MessageRoleUser,
				Parts: []a2a.Part{
					a2a.TextPart("test user message"),
				},
			},
			Metadata: map[string]any{
				"key": "value",
			},
		}
		task, err := store.UpsertTask(ctx, taskParams)
		require.NoError(t, err, "failed to upsert task")
		require.Equal(t, task.ID, taskID)
		require.Len(t, task.History, 1, "task history should be not empty")
		require.EqualValues(t, task.History[0].Parts[0].Text, a2a.Ptr("test user message"))
		require.EqualValues(t, task.Metadata["key"], "value")
	})

	t.Run("GetTask", func(t *testing.T) {
		task, err := store.GetTask(ctx, taskID, nil)
		require.NoError(t, err, "failed to get task")
		require.Equal(t, task.ID, taskID)
		require.Len(t, task.History, 1, "task history should be not empty")
		require.EqualValues(t, task.History[0].Parts[0].Text, a2a.Ptr("test user message"))
		require.EqualValues(t, task.Metadata["key"], "value")
	})

	t.Run("AppendHistory", func(t *testing.T) {
		message := a2a.Message{
			Role: a2a.MessageRoleAgent,
			Parts: []a2a.Part{
				a2a.TextPart("test agent message"),
			},
		}
		err := store.AppendHistory(ctx, taskID, message)
		require.NoError(t, err, "failed to append history")

		task, err := store.GetTask(ctx, taskID, aws.Int(10))
		require.NoError(t, err, "failed to get task after appending history")
		require.Len(t, task.History, 2)
		require.Equal(t, task.History[1].Parts[0].Text, a2a.Ptr("test agent message"))
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		err := store.UpdateStatus(ctx, taskID, a2a.TaskStatus{
			State: a2a.TaskStateCompleted,
		})
		require.NoError(t, err, "failed to update status")

		task, err := store.GetTask(ctx, taskID, aws.Int(10))
		require.NoError(t, err, "failed to get task after updating status")
		require.Equal(t, task.Status.State, a2a.TaskStateCompleted)
		require.NotNil(t, task.Status.Timestamp, "task status timestamp should be nil")
	})

	t.Run("CreateTaskPushNotification", func(t *testing.T) {
		cfg := &a2a.TaskPushNotificationConfig{
			ID: taskID,
			PushNotificationConfig: a2a.PushNotificationConfig{
				URL: "http://example.com",
			},
		}
		err := store.CreateTaskPushNotification(ctx, cfg)
		require.NoError(t, err, "failed to create push notification config")

		retrievedCfg, err := store.GetTaskPushNotification(ctx, taskID)
		require.NoError(t, err, "failed to get push notification config")
		require.Equal(t, retrievedCfg.ID, taskID)
		require.Equal(t, retrievedCfg.PushNotificationConfig.URL, "http://example.com")
	})
}
