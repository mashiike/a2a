package a2a

import (
	"context"
	"sync"
	"time"
)

// Store defines the interface for managing tasks and their associated data.
type Store interface {
	// UpsertTask creates or updates a task in the store.
	// If the task already exists, it appends a message to the task's history and updates its metadata.
	// If the task does not exist, it creates a new task with a "submitted" status.
	// Returns the task with the applied updates.
	UpsertTask(ctx context.Context, params TaskSendParams) (*Task, error)

	// GetTask retrieves a task by its ID.
	GetTask(ctx context.Context, taskID string, historyLength *int) (*Task, error)

	// AppendHistory appends a message to the history of a given task ID.
	AppendHistory(ctx context.Context, taskID string, message Message) error

	// UpdateStatus updates the status of a task by its ID.
	UpdateStatus(ctx context.Context, taskID string, status TaskStatus) error

	// UpdateArtifact updates or appends an artifact to a task by its ID.
	UpdateArtifact(ctx context.Context, taskID string, artifact Artifact) error
}

// PushNotificationStore defines the interface for managing push notifications for tasks.
type PushNotificationStore interface {
	// CreateTaskPushNotification creates a push notification configuration for a task.
	CreateTaskPushNotification(ctx context.Context, cfg *TaskPushNotificationConfig) error

	// GetTaskPushNotification retrieves the push notification configuration for a task by its ID.
	GetTaskPushNotification(ctx context.Context, taskID string) (*TaskPushNotificationConfig, error)
}

// MutateTask is utility function for implementing the Store interface.
// if cur is nil, it will return the new task.
// if cur is not nil, it will return the updated task.
func MutateTask(cur *Task, params TaskSendParams) *Task {
	if cur == nil {
		task := &Task{
			ID:       params.ID,
			Metadata: params.Metadata,
			Status: TaskStatus{
				State: TaskStateSubmitted,
			},
			History: []Message{
				params.Message,
			},
			Artifacts: []Artifact{},
		}
		if params.SessionID != nil {
			task.SessionID = *params.SessionID
		}
		return task
	}
	if cur.Status.State == TaskStateCompleted {
		return cur
	}
	cloned := *cur
	// Update the task with the new parameters
	for k, v := range params.Metadata {
		if v == nil {
			delete(cloned.Metadata, k)
		} else {
			cloned.Metadata[k] = v
		}
	}
	cloned.History = append(cloned.History, params.Message)
	return &cloned
}

// TruncateHistory is utility function for implementing the Store interface.
// it will truncate the history of the task to the specified length.
// if length is negative, it will return all messages.
// if length is nil, it will return the task as is.
// if length is 0, it will return the task with an empty history.
// if length is greater than the current history length, truncate and return only last N messages.
func TruncateHistory(task *Task, length *int) *Task {
	if task == nil {
		return nil
	}
	cloned := *task
	if length == nil || *length < 0 {
		return &cloned
	}
	if *length == 0 {
		cloned.History = []Message{}
		return &cloned
	}
	if len(cloned.History) > *length {
		cloned.History = append([]Message{}, cloned.History[len(cloned.History)-*length:]...)
	}
	return &cloned
}

// InMemoryStore is an in-memory implementation of the Store and PushNotificationStore interfaces.
type InMemoryStore struct {
	mu                sync.RWMutex
	initOnce          sync.Once
	tasks             map[string]*Task
	pushNotifications map[string]*TaskPushNotificationConfig
}

// init initializes the internal data structures of the InMemoryStore.
func (s *InMemoryStore) init() {
	s.initOnce.Do(func() {
		s.tasks = make(map[string]*Task)
		s.pushNotifications = make(map[string]*TaskPushNotificationConfig)
	})
}

// CreateTask creates a new task in the in-memory store.
func (s *InMemoryStore) UpsertTask(ctx context.Context, params TaskSendParams) (*Task, error) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.tasks[params.ID]
	stored := MutateTask(cur, params)
	s.tasks[params.ID] = stored
	return TruncateHistory(stored, params.HistoryLength), nil
}

// GetTask retrieves a task by its ID from the in-memory store.
func (s *InMemoryStore) GetTask(ctx context.Context, taskID string, historyLength *int) (*Task, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if task, ok := s.tasks[taskID]; ok {
		return TruncateHistory(task, historyLength), nil
	}
	return nil, ErrTaskNotFound
}

// AppendHistory appends a message to the history of a given task ID.
func (s *InMemoryStore) AppendHistory(ctx context.Context, taskID string, message Message) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	task.History = append(task.History, message)
	s.tasks[taskID] = task
	return nil
}

func MutateTaskStatus(task *Task, status TaskStatus) (*Task, bool, error) {
	if task == nil {
		return nil, false, ErrTaskNotFound
	}
	if status.Timestamp == nil {
		// Set the timestamp to the current time
		timestamp := time.Now().Format(time.RFC3339)
		status.Timestamp = &timestamp
	}
	if task.Status.Timestamp != nil {
		before, err := time.Parse(time.RFC3339, *task.Status.Timestamp)
		if err != nil {
			return nil, false, err
		}
		after, err := time.Parse(time.RFC3339, *status.Timestamp)
		if err != nil {
			return nil, false, err
		}
		if after.Before(before) {
			// Ignore the update if the new timestamp is before the current one
			return task, false, nil
		}
	}
	cloned := *task
	cloned.Status = status
	return &cloned, true, nil
}

// UpdateStatus updates the status of a task by its ID.
func (s *InMemoryStore) UpdateStatus(ctx context.Context, taskID string, status TaskStatus) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok || task == nil {
		return ErrTaskNotFound
	}
	after, isUpdated, err := MutateTaskStatus(task, status)
	if err != nil {
		return err
	}
	if !isUpdated {
		return nil
	}
	s.tasks[taskID] = after
	return nil
}

func MutateTaskArtifact(task *Task, artifact Artifact) (*Task, error) {
	if task == nil {
		return nil, ErrTaskNotFound
	}
	if artifact.Index < 0 {
		return nil, ErrTaskArtifactIndexInvalid
	}
	cloned := *task
	if artifact.Append != nil && *artifact.Append {
		if artifact.Index < len(task.Artifacts) {
			cloned.Artifacts[artifact.Index].Parts = append(cloned.Artifacts[artifact.Index].Parts, artifact.Parts...)
			return &cloned, nil
		}
		return nil, ErrTaskArtifactNotFound
	}
	if artifact.Index < len(task.Artifacts) {
		// Update existing artifact
		cloned.Artifacts[artifact.Index] = artifact
	} else {
		// resize the artifacts slice
		artifacts := make([]Artifact, artifact.Index+1)
		copy(artifacts, cloned.Artifacts)
		artifacts[artifact.Index] = artifact
		cloned.Artifacts = artifacts
	}
	return &cloned, nil
}

// UpdateArtifact updates or appends an artifact to a task by its ID.
func (s *InMemoryStore) UpdateArtifact(ctx context.Context, taskID string, artifact Artifact) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	after, err := MutateTaskArtifact(task, artifact)
	if err != nil {
		return err
	}
	s.tasks[taskID] = after
	return nil
}

// CreateTaskPushNotification creates a push notification configuration for a task.
func (s *InMemoryStore) CreateTaskPushNotification(ctx context.Context, cfg *TaskPushNotificationConfig) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushNotifications[cfg.ID] = cfg
	return nil
}

// GetTaskPushNotification retrieves the push notification configuration for a task by its ID.
func (s *InMemoryStore) GetTaskPushNotification(ctx context.Context, taskID string) (*TaskPushNotificationConfig, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cfg, ok := s.pushNotifications[taskID]; ok {
		// clone cfg
		cloned := *cfg
		return &cloned, nil
	}
	return nil, ErrTaskPushNotificationNotConfigured
}
