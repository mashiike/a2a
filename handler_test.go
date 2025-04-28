package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/mashiike/a2a/jsonrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_TasksSendSync(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		assert.Equal(t, "test-task-id", task.ID)
		assert.Equal(t, "test-session-id", task.SessionID)
		assert.Len(t, task.History, 1)
		msg := task.History[0]
		assert.Equal(t, MessageRoleUser, msg.Role)
		assert.Len(t, msg.Parts, 1)
		assert.Equal(t, PartTypeText, msg.Parts[0].Type)
		assert.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)
		httpReq, rpcReq := FromContext(ctx)
		assert.Equal(t, "/", httpReq.URL.Path)
		assert.Equal(t, "POST", httpReq.Method)
		assert.Equal(t, jsonrpc.MethodTasksSend, rpcReq.Method)
		m.WriteArtifact(
			ctx,
			Artifact{
				Index: 0,
				Parts: []Part{
					TextPart("Hello, I am a test agent."),
				},
			},
			nil,
		)
		m.SetStatus(ctx, TaskStatus{
			State: TaskStateCompleted,
		}, nil)
		return nil
	})
	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	params := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}
	task, err := client.SendTask(ctx, params)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", task.ID)
	require.Equal(t, "test-session-id", task.SessionID)
	require.Len(t, task.History, 1)
	msg := task.History[0]
	require.Equal(t, MessageRoleUser, msg.Role)
	require.Len(t, msg.Parts, 1)
	require.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)
	require.Len(t, task.Artifacts, 1)
	require.Equal(t, 0, task.Artifacts[0].Index)
	require.Len(t, task.Artifacts[0].Parts, 1)
	require.Equal(t, PartTypeText, task.Artifacts[0].Parts[0].Type)
	require.EqualValues(t, Ptr("Hello, I am a test agent."), task.Artifacts[0].Parts[0].Text)
	require.Equal(t, TaskStateCompleted, task.Status.State)
}

func TestHandler_TasksSendAsync(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	var wg sync.WaitGroup
	done := make(chan struct{})
	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		assert.Equal(t, "test-task-id", task.ID)
		assert.Equal(t, "test-session-id", task.SessionID)
		assert.Len(t, task.History, 1)
		msg := task.History[0]
		assert.Equal(t, MessageRoleUser, msg.Role)
		assert.Len(t, msg.Parts, 1)
		assert.Equal(t, PartTypeText, msg.Parts[0].Type)
		assert.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)

		// Start asynchronous processing
		m.SetStatus(ctx, TaskStatus{
			State: TaskStateWorking,
		}, nil)

		wg.Add(1) // Increment WaitGroup counter
		go func() {
			defer wg.Done() // Decrement WaitGroup counter when done
			<-done
			m.WriteArtifact(
				ctx,
				Artifact{
					Index: 0,
					Parts: []Part{
						TextPart("Hello, I am a test agent."),
					},
				},
				nil,
			)
			m.SetStatus(ctx, TaskStatus{
				State: TaskStateCompleted,
			}, nil)
		}()
		return nil
	})
	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	params := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}
	task, err := client.SendTask(ctx, params)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", task.ID)
	require.Equal(t, "test-session-id", task.SessionID)
	require.Len(t, task.History, 1)
	msg := task.History[0]
	require.Equal(t, MessageRoleUser, msg.Role)
	require.Len(t, msg.Parts, 1)
	require.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)
	require.Equal(t, TaskStateWorking, task.Status.State)

	// Complete asynchronous processing
	close(done) // Close the channel to signal completion
	wg.Wait()   // Wait for the background goroutine to finish

	// Verify completion state
	task, err = client.GetTask(ctx, TaskQueryParams{ID: "test-task-id"})
	require.NoError(t, err)
	require.Equal(t, TaskStateCompleted, task.Status.State)
	require.Len(t, task.Artifacts, 1)
	require.Equal(t, 0, task.Artifacts[0].Index)
	require.Len(t, task.Artifacts[0].Parts, 1)
	require.Equal(t, PartTypeText, task.Artifacts[0].Parts[0].Type)
	require.EqualValues(t, Ptr("Hello, I am a test agent."), task.Artifacts[0].Parts[0].Text)
}

func TestHandler_TasksCancel(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	cancelCtxCalled := make(chan struct{})
	var wg sync.WaitGroup
	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		assert.Equal(t, "test-task-id", task.ID)
		assert.Equal(t, "test-session-id", task.SessionID)
		assert.Len(t, task.History, 1)
		msg := task.History[0]
		assert.Equal(t, MessageRoleUser, msg.Role)
		assert.Len(t, msg.Parts, 1)
		assert.Equal(t, PartTypeText, msg.Parts[0].Type)
		assert.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)

		// Start background processing
		m.SetStatus(ctx, TaskStatus{
			State: TaskStateWorking,
		}, nil)

		wg.Add(1) // Increment WaitGroup counter
		go func() {
			defer wg.Done() // Decrement WaitGroup counter when done
			select {
			case <-ctx.Done():
				// Notify that the context was canceled
				close(cancelCtxCalled)
				return
			case <-time.After(5 * time.Second): // Simulate long-running task
				m.SetStatus(ctx, TaskStatus{
					State: TaskStateCompleted,
				}, nil)
			}
		}()
		return nil
	})
	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
			TaskStatePollingInterval: 100 * time.Millisecond, // detect cancelation quickly
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	params := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}

	// Create the task
	task, err := client.SendTask(ctx, params)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", task.ID)
	require.Equal(t, "test-session-id", task.SessionID)
	require.Equal(t, TaskStateWorking, task.Status.State)

	// Cancel the task
	_, err = client.CancelTask(ctx, TaskIDParams{ID: "test-task-id"})
	require.NoError(t, err)

	// Verify that the agent's context was canceled
	select {
	case <-cancelCtxCalled:
		// Context was canceled as expected
	case <-time.After(1 * time.Second):
		t.Fatal("Agent context was not canceled")
	}

	// Wait for the background goroutine to finish
	wg.Wait()

	// Verify the task state is canceled
	task, err = client.GetTask(ctx, TaskQueryParams{ID: "test-task-id"})
	require.NoError(t, err)
	require.Equal(t, TaskStateCanceled, task.Status.State)
}

func TestHandler_TasksSendSubscribeSync(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		assert.Equal(t, "test-task-id", task.ID)
		assert.Equal(t, "test-session-id", task.SessionID)
		assert.Len(t, task.History, 1)
		msg := task.History[0]
		assert.Equal(t, MessageRoleUser, msg.Role)
		assert.Len(t, msg.Parts, 1)
		assert.Equal(t, PartTypeText, msg.Parts[0].Type)
		assert.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)

		// Write an artifact and set the task status to completed
		m.WriteArtifact(
			ctx,
			Artifact{
				Index: 0,
				Parts: []Part{
					TextPart("Hello, I am a test agent."),
				},
			},
			nil,
		)
		m.SetStatus(ctx, TaskStatus{
			State: TaskStateCompleted,
		}, nil)
		return nil
	})
	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
			TaskStatePollingInterval: 100 * time.Millisecond, // detect state changes quickly
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	params := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}

	// Subscribe to the task
	eventCh, err := client.SendTaskSubscribe(ctx, params)
	require.NoError(t, err)

	// Collect all events
	var events []StreamingEvent
	for event := range eventCh {
		events = append(events, event)
	}

	// Verify the sequence of events
	require.Len(t, events, 2)

	// Verify the first event is artifactUpdated
	require.Equal(t, "artifactUpdated", events[0].EventType())
	artifact := events[0].ArtifactUpdated.Artifact
	require.Equal(t, 0, artifact.Index)
	require.Len(t, artifact.Parts, 1)
	require.Equal(t, PartTypeText, artifact.Parts[0].Type)
	require.EqualValues(t, Ptr("Hello, I am a test agent."), artifact.Parts[0].Text)

	// Verify the second event is completed
	require.Equal(t, "statusUpdated", events[1].EventType())
	require.Equal(t, TaskStateCompleted, events[1].StatusUpdated.Status.State)
}

func TestHandler_TasksResubscribe(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	var wg sync.WaitGroup
	done := make(chan struct{})
	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		// Set initial state to Working with a "Waiting Ready" message
		m.SetStatus(ctx, TaskStatus{
			State: TaskStateWorking,
			Message: &Message{
				Role: MessageRoleAgent,
				Parts: []Part{
					TextPart("Waiting Ready"),
				},
			},
		}, nil)
		wg.Add(1)
		go func() {
			ctx := context.Background()
			defer wg.Done()
			// Wait for the signal to proceed
			<-done
			// Update state to Working with a "Processing..." message
			m.SetStatus(ctx, TaskStatus{
				State: TaskStateWorking,
				Message: &Message{
					Role: MessageRoleAgent,
					Parts: []Part{
						TextPart("Processing..."),
					},
				},
			}, nil)
			// Finalize with Completed state and an artifact
			m.WriteArtifact(ctx, Artifact{
				Index: 2,
				Parts: []Part{
					TextPart("Final result."),
				},
			}, nil)
			m.SetStatus(ctx, TaskStatus{
				State: TaskStateCompleted,
			}, nil)
		}()
		return nil
	})
	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
			TaskStatePollingInterval: 100 * time.Millisecond,
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()
	params := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}

	// Subscribe to the task
	subscribeCtx, cancel := context.WithCancel(ctx)
	eventCh, err := client.SendTaskSubscribe(subscribeCtx, params)
	require.NoError(t, err)

	// Read the first event (Working state with "Waiting Ready")
	event := <-eventCh
	require.Equal(t, "statusUpdated", event.EventType())
	require.Equal(t, TaskStateWorking, event.StatusUpdated.Status.State)
	require.EqualValues(t, Ptr("Waiting Ready"), event.StatusUpdated.Status.Message.Parts[0].Text)

	// Cancel the subscription
	cancel()
	var events []StreamingEvent
	for event := range eventCh {
		events = append(events, event)
	}
	time.Sleep(500 * time.Millisecond) // Wait for the channel to close
	// Resubscribe to the task
	resubscribeCtx := context.Background()
	eventCh, err = client.ResubscribeTask(resubscribeCtx, TaskIDParams{
		ID: "test-task-id",
	})
	require.NoError(t, err)
	// Close the channel to allow the agent to proceed
	close(done)

	// Collect remaining events
	for event := range eventCh {
		events = append(events, event)
	}

	// Verify the sequence of events
	require.Len(t, events, 3)

	// Verify the second event (Working state with "Processing...")
	require.Equal(t, "statusUpdated", events[0].EventType())
	require.Equal(t, TaskStateWorking, events[0].StatusUpdated.Status.State)
	require.EqualValues(t, Ptr("Processing..."), events[0].StatusUpdated.Status.Message.Parts[0].Text)

	// Verify the third event (Completed state with "Final result.")
	require.Equal(t, "artifactUpdated", events[1].EventType())
	require.EqualValues(t, Ptr("Final result."), events[1].ArtifactUpdated.Artifact.Parts[0].Text)

	// Verify the final state is Completed
	require.Equal(t, "statusUpdated", events[2].EventType())
	require.Equal(t, TaskStateCompleted, events[2].StatusUpdated.Status.State)
	wg.Wait()
}

func TestHandler_TasksPushNotification(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	var wg sync.WaitGroup
	done := make(chan struct{})
	var receivedNotification []byte
	notificationServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		receivedNotification = body
		w.WriteHeader(http.StatusOK)
	}))
	defer notificationServer.Close()

	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		assert.Equal(t, "test-task-id", task.ID)
		assert.Equal(t, "test-session-id", task.SessionID)
		assert.Len(t, task.History, 1)
		msg := task.History[0]
		assert.Equal(t, MessageRoleUser, msg.Role)
		assert.Len(t, msg.Parts, 1)
		assert.Equal(t, PartTypeText, msg.Parts[0].Type)
		assert.EqualValues(t, Ptr("who are you?"), msg.Parts[0].Text)

		// Start background processing
		m.SetStatus(ctx, TaskStatus{
			State: TaskStateWorking,
		}, nil)

		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			<-done
			m.WriteArtifact(
				ctx,
				Artifact{
					Index: 0,
					Parts: []Part{
						TextPart("Hello, I am a test agent."),
					},
				},
				nil,
			)
			m.SetStatus(ctx, TaskStatus{
				State: TaskStateCompleted,
			}, nil)
		}()
		return nil
	})

	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
			TaskStatePollingInterval: 100 * time.Millisecond,
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()

	ctx := context.Background()
	params := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}

	// Start the task
	task, err := client.SendTask(ctx, params)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", task.ID)
	require.Equal(t, "test-session-id", task.SessionID)
	require.Equal(t, TaskStateWorking, task.Status.State)

	// Set PushNotification
	pushNotificationConfig := &TaskPushNotificationConfig{
		ID: "test-task-id",
		PushNotificationConfig: PushNotificationConfig{
			URL: notificationServer.URL,
		},
	}
	_, err = client.SetPushNotification(ctx, pushNotificationConfig)
	require.NoError(t, err)

	// Get PushNotification and verify
	retrievedConfig, err := client.GetPushNotification(ctx, TaskIDParams{ID: "test-task-id"})
	require.NoError(t, err)
	require.Equal(t, pushNotificationConfig.PushNotificationConfig.URL, retrievedConfig.PushNotificationConfig.URL)

	// Complete the task
	close(done)
	wg.Wait()

	// Verify the notification payload
	require.NotNil(t, receivedNotification)
	var receivedTask Task
	err = json.Unmarshal(receivedNotification, &receivedTask)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", receivedTask.ID)
	require.Equal(t, TaskStateCompleted, receivedTask.Status.State)
	require.Len(t, receivedTask.Artifacts, 1)
	require.Equal(t, 0, receivedTask.Artifacts[0].Index)
	require.Len(t, receivedTask.Artifacts[0].Parts, 1)
	require.Equal(t, PartTypeText, receivedTask.Artifacts[0].Parts[0].Type)
	require.EqualValues(t, Ptr("Hello, I am a test agent."), receivedTask.Artifacts[0].Parts[0].Text)
}

func TestHandler_MultiTurnConversation(t *testing.T) {
	card := &AgentCard{
		Name:               "Test Agent",
		URL:                "http://0.0.0.0:8080", // dummy URL
		Version:            "0.0.0",
		DefaultInputModes:  []string{"text"},
		DefaultOutputModes: []string{"text"},
		Skills: []Skill{
			{
				ID:   "test-skill",
				Name: "Test Skill",
				Tags: []string{"test"},
				Examples: []string{
					"example1",
					"example2",
				},
			},
		},
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	defer func() {
		t.Log(buf.String())
	}()
	agent := AgentFunc(func(ctx context.Context, m TaskManager, task *Task) error {
		if len(task.History) == 1 {
			// First turn: return InputRequired
			m.SetStatus(ctx, TaskStatus{
				State: TaskStateInputRequired,
				Message: &Message{
					Role: MessageRoleAgent,
					Parts: []Part{
						TextPart("I am test agent, who are you?"),
					},
				},
			}, nil)
		} else {
			// Second turn: return Completed
			m.SetStatus(ctx, TaskStatus{
				State: TaskStateCompleted,
			}, nil)
		}
		return nil
	})
	handler, err := NewHandler(
		card,
		agent,
		&HandlerOptions{
			Logger: logger,
			SessionIDGenerator: func(_ *http.Request) string {
				return "test-session-id"
			},
		},
	)
	require.NoError(t, err)
	server := httptest.NewServer(handler)
	defer server.Close()
	card.URL = server.URL

	client, err := NewClient(server.URL)
	require.NoError(t, err)
	defer client.Close()
	ctx := context.Background()

	// First turn
	firstParams := TaskSendParams{
		ID: "test-task-id",
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("Hello, who are you?"),
			},
		},
		HistoryLength: Ptr(10),
	}
	firstTask, err := client.SendTask(ctx, firstParams)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", firstTask.ID)
	require.Equal(t, "test-session-id", firstTask.SessionID)
	require.Len(t, firstTask.History, 2)
	require.Equal(t, MessageRoleUser, firstTask.History[0].Role)
	require.EqualValues(t, Ptr("Hello, who are you?"), firstTask.History[0].Parts[0].Text)
	require.Equal(t, TaskStateInputRequired, firstTask.Status.State)
	require.Equal(t, MessageRoleAgent, firstTask.History[1].Role)
	require.EqualValues(t, Ptr("I am test agent, who are you?"), firstTask.History[1].Parts[0].Text)

	// Second turn
	secondParams := TaskSendParams{
		ID:        "test-task-id",
		SessionID: Ptr(firstTask.SessionID),
		Message: Message{
			Role: MessageRoleUser,
			Parts: []Part{
				TextPart("Can you tell me more?"),
			},
		},
		HistoryLength: Ptr(10),
	}
	secondTask, err := client.SendTask(ctx, secondParams)
	require.NoError(t, err)
	require.Equal(t, "test-task-id", secondTask.ID)
	require.Equal(t, firstTask.SessionID, secondTask.SessionID) // Ensure SessionID is the same
	require.Len(t, secondTask.History, 3)
	require.Equal(t, MessageRoleUser, secondTask.History[0].Role)
	require.EqualValues(t, Ptr("Hello, who are you?"), secondTask.History[0].Parts[0].Text)
	require.Equal(t, MessageRoleAgent, secondTask.History[1].Role)
	require.EqualValues(t, Ptr("I am test agent, who are you?"), secondTask.History[1].Parts[0].Text)
	require.Equal(t, MessageRoleUser, secondTask.History[2].Role)
	require.EqualValues(t, Ptr("Can you tell me more?"), secondTask.History[2].Parts[0].Text)
	require.Equal(t, TaskStateCompleted, secondTask.Status.State)
}
