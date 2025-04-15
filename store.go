package a2a

import (
	"context"
	"sync"
	"time"
)

type Store interface {
	GetHistory(ctx context.Context, sessionID string, historyLength int) ([]Message, error)
	AppendHistory(ctx context.Context, sessionID string, message Message) error
	CreateTask(ctx context.Context, task *Task) error
	GetTask(ctx context.Context, taskID string) (*Task, error)
	UpdateStatus(ctx context.Context, taskID string, status TaskStatus) error
	UpdateArtifact(ctx context.Context, taskID string, artifact Artifact) error
}

type PushNotificationStore interface {
	CreateTaskPushNotification(ctx context.Context, cfg *TaskPushNotificationConfig) error
	GetTaskPushNotification(ctx context.Context, taskID string) (*TaskPushNotificationConfig, error)
}

type InMemoryStore struct {
	mu                sync.RWMutex
	initOnce          sync.Once
	tasks             map[string]*Task
	history           map[string][]Message
	pushNotifications map[string]*TaskPushNotificationConfig
}

func (s *InMemoryStore) init() {
	s.initOnce.Do(func() {
		s.tasks = make(map[string]*Task)
		s.history = make(map[string][]Message)
		s.pushNotifications = make(map[string]*TaskPushNotificationConfig)
	})
}

func (s *InMemoryStore) CreateTask(ctx context.Context, task *Task) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	//clone task
	cloned := *task
	s.tasks[task.ID] = &cloned
	return nil
}

func (s *InMemoryStore) GetTask(ctx context.Context, taskID string) (*Task, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if task, ok := s.tasks[taskID]; ok {
		// clone task
		cloned := *task
		return &cloned, nil
	}
	return nil, ErrTaskNotFound
}

func (s *InMemoryStore) AppendHistory(ctx context.Context, sessionID string, message Message) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.history[sessionID]; !ok {
		s.history[sessionID] = []Message{}
	}
	s.history[sessionID] = append(s.history[sessionID], message)
	return nil
}

func (s *InMemoryStore) GetHistory(ctx context.Context, sessionID string, historyLength int) ([]Message, error) {
	s.init()
	s.mu.RLock()
	defer s.mu.RUnlock()
	if messages, ok := s.history[sessionID]; ok {
		if historyLength < 0 {
			return messages, nil
		}
		// return the last N messages
		if len(messages) > historyLength {
			return messages[len(messages)-historyLength:], nil
		}
		return messages, nil
	}
	return []Message{}, nil
}

func (s *InMemoryStore) UpdateStatus(ctx context.Context, taskID string, status TaskStatus) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if status.Timestamp == nil {
		// Set the timestamp to the current time
		timestamp := time.Now().Format(time.RFC3339)
		status.Timestamp = &timestamp
	}
	if task.Status.Timestamp != nil {
		before, _ := time.Parse(time.RFC3339, *task.Status.Timestamp)
		after, _ := time.Parse(time.RFC3339, *status.Timestamp)
		if after.Before(before) {
			// Ignore the update if the new timestamp is before the current one
			return nil
		}
	}
	task.Status = status
	s.tasks[taskID] = task
	return nil
}

func (s *InMemoryStore) UpdateArtifact(ctx context.Context, taskID string, artifact Artifact) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	if task, ok := s.tasks[taskID]; ok {
		if artifact.Index < len(task.Artifacts) {
			// Update existing artifact
			task.Artifacts[artifact.Index] = artifact
		} else {
			// resize the artifacts slice
			artifacts := make([]Artifact, artifact.Index+1)
			copy(artifacts, task.Artifacts)
			artifacts[artifact.Index] = artifact
			task.Artifacts = artifacts
		}
		s.tasks[taskID] = task
		return nil
	}
	return ErrTaskNotFound
}

func (s *InMemoryStore) CreateTaskPushNotification(ctx context.Context, cfg *TaskPushNotificationConfig) error {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pushNotifications[cfg.ID] = cfg
	return nil
}

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
