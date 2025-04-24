package a2a

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// AgentCard represents the key information about an agent.
// Specification: https://google.github.io/A2A/#/documentation?id=agent-card
type AgentCard struct {
	// Human-readable name of the agent.
	Name string `json:"name"`
	// A human-readable description of the agent's functionality.
	Description string `json:"description"`
	// URL where the agent is hosted.
	URL string `json:"url"`
	// Optional provider information.
	Provider *Provider `json:"provider,omitempty"`
	// Version of the agent.
	Version string `json:"version"`
	// Optional URL to the agent's documentation.
	DocumentationURL *string `json:"documentationUrl,omitempty"`
	// Capabilities supported by the agent.
	Capabilities Capabilities `json:"capabilities"`
	// Authentication requirements for the agent.
	Authentication Authentication `json:"authentication"`
	// Default input modes supported by the agent.
	DefaultInputModes []string `json:"defaultInputModes"`
	// Default output modes supported by the agent.
	DefaultOutputModes []string `json:"defaultOutputModes"`
	// Skills that the agent can perform.
	Skills []Skill `json:"skills"`
}

// Validate checks if the required fields in AgentCard are set.
func (a *AgentCard) Validate() error {
	if a.Name == "" {
		return errors.New("name is required")
	}
	if a.URL == "" {
		return errors.New("url is required")
	}
	if a.Version == "" {
		return errors.New("version is required")
	}
	if err := a.Capabilities.Validate(); err != nil {
		return fmt.Errorf("capabilities validation failed: %w", err)
	}
	if len(a.Skills) == 0 {
		return errors.New("skills must have at least one entry")
	}
	for _, skill := range a.Skills {
		if err := skill.Validate(); err != nil {
			return fmt.Errorf("skill validation failed: %w", err)
		}
	}
	return nil
}

// Provider represents the service provider of the agent.
type Provider struct {
	// Organization name of the provider.
	Organization string `json:"organization"`
	// URL of the provider.
	URL string `json:"url"`
}

// Validate checks if the required fields in Provider are set.
func (p *Provider) Validate() error {
	if p.Organization == "" {
		return errors.New("organization is required")
	}
	return nil
}

// Capabilities represents the optional capabilities supported by the agent.
type Capabilities struct {
	// True if the agent supports SSE.
	Streaming *bool `json:"streaming,omitempty"`
	// True if the agent can notify updates to the client.
	PushNotifications *bool `json:"pushNotifications,omitempty"`
	// True if the agent exposes status change history for tasks.
	StateTransitionHistory *bool `json:"stateTransitionHistory,omitempty"`
}

// Validate checks if the required fields in Capabilities are set.
func (c *Capabilities) Validate() error {
	// No specific validation required as all fields are optional
	return nil
}

// Authentication represents the authentication requirements for the agent.
type Authentication struct {
	// Authentication schemes supported by the agent (e.g., Basic, Bearer).
	Schemes []string `json:"schemes"`
	// Optional credentials for private cards.
	Credentials *string `json:"credentials,omitempty"`
}

// Validate checks if the required fields in Authentication are set.
func (a *Authentication) Validate() error {
	if len(a.Schemes) == 0 {
		return errors.New("schemes must have at least one entry")
	}
	return nil
}

// Skill represents a unit of capability that an agent can perform.
type Skill struct {
	// Unique identifier for the skill.
	ID string `json:"id"`
	// Human-readable name of the skill.
	Name string `json:"name"`
	// Description of the skill.
	Description string `json:"description"`
	// Tags describing the skill's capabilities.
	Tags []string `json:"tags"`
	// Optional example scenarios for the skill.
	Examples []string `json:"examples,omitempty"`
	// Optional input modes supported by the skill.
	InputModes []string `json:"inputModes,omitempty"`
	// Optional output modes supported by the skill.
	OutputModes []string `json:"outputModes,omitempty"`
}

// Validate checks if the required fields in Skill are set.
func (s *Skill) Validate() error {
	if s.ID == "" {
		return errors.New("skill id is required")
	}
	if s.Name == "" {
		return errors.New("skill name is required")
	}
	// Description, Tags, Examples, InputModes, and OutputModes are optional
	return nil
}

// Task represents a task handled by the agent.
type Task struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	Status    TaskStatus     `json:"status"`
	Message   *Message       `json:"-"` // incoming message on tasks/send or tasks/sendSubscribe
	History   []Message      `json:"history,omitempty"`
	Artifacts []Artifact     `json:"artifacts,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// Validate checks if the required fields in Task are set correctly.
func (t *Task) Validate() error {
	if t.ID == "" {
		return errors.New("id is required")
	}
	if t.SessionID == "" {
		return errors.New("sessionId is required")
	}
	if err := t.Status.Validate(); err != nil {
		return err
	}
	for _, message := range t.History {
		if err := message.Validate(); err != nil {
			return err
		}
	}
	for _, artifact := range t.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	if t.Metadata == nil {
		t.Metadata = make(map[string]any)
	}
	return nil
}

// TaskStatus represents the current status of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *Message  `json:"message,omitempty"`
	Timestamp *string   `json:"timestamp,omitempty"` // ISO datetime value
}

// Validate checks if the required fields in TaskStatus are set correctly.
func (ts *TaskStatus) Validate() error {
	if ts.State == "" {
		return errors.New("state is required")
	}
	if ts.Message != nil {
		if err := ts.Message.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// TaskStatusUpdateEvent represents a status update event for a task.
type TaskStatusUpdateEvent struct {
	ID       string         `json:"id"`
	Status   TaskStatus     `json:"status"`
	Final    bool           `json:"final"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks if the required fields in TaskStatusUpdateEvent are set correctly.
func (e *TaskStatusUpdateEvent) Validate() error {
	if e.ID == "" {
		return errors.New("id is required")
	}
	if err := e.Status.Validate(); err != nil {
		return err
	}
	if e.Metadata == nil {
		e.Metadata = make(map[string]any)
	}
	return nil
}

// TaskArtifactUpdateEvent represents an artifact update event for a task.
type TaskArtifactUpdateEvent struct {
	ID       string         `json:"id"`
	Artifact Artifact       `json:"artifact"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks if the required fields in TaskArtifactUpdateEvent are set correctly.
func (e *TaskArtifactUpdateEvent) Validate() error {
	if e.ID == "" {
		return errors.New("id is required")
	}
	if err := e.Artifact.Validate(); err != nil {
		return err
	}
	if e.Metadata == nil {
		e.Metadata = make(map[string]any)
	}
	return nil
}

// StreamingEvent represents a streaming event for a task.
type StreamingEvent struct {
	StatusUpdated   *TaskStatusUpdateEvent   `json:"status_updated,omitempty"`
	ArtifactUpdated *TaskArtifactUpdateEvent `json:"artifact_updated,omitempty"`
}

func (e *StreamingEvent) UnmarshalJSON(data []byte) error {
	var statusUpdated TaskStatusUpdateEvent
	if err := json.Unmarshal(data, &statusUpdated); err == nil {
		if err := statusUpdated.Validate(); err == nil {
			e.StatusUpdated = &statusUpdated
			return nil
		}
	}
	var artifactUpdated TaskArtifactUpdateEvent
	if err := json.Unmarshal(data, &artifactUpdated); err == nil {
		if err := artifactUpdated.Validate(); err == nil {
			e.ArtifactUpdated = &artifactUpdated
			return nil
		}
	}
	return errors.New("invalid streaming event")
}

func (e *StreamingEvent) MarshalJSON() ([]byte, error) {
	if e.StatusUpdated != nil {
		return json.Marshal(e.StatusUpdated)
	}
	if e.ArtifactUpdated != nil {
		return json.Marshal(e.ArtifactUpdated)
	}
	return nil, errors.New("no event to marshal")
}

func (e *StreamingEvent) EventType() string {
	if e.StatusUpdated != nil {
		return "statusUpdated"
	}
	if e.ArtifactUpdated != nil {
		return "artifactUpdated"
	}
	return "unknown"
}

// TaskSendParams represents parameters sent by the client to create, continue, or restart a task.
type TaskSendParams struct {
	ID               string                  `json:"id"`
	SessionID        *string                 `json:"sessionId,omitempty"`
	Message          Message                 `json:"message"`
	HistoryLength    *int                    `json:"historyLength,omitempty"`
	PushNotification *PushNotificationConfig `json:"pushNotification,omitempty"`
	Metadata         map[string]any          `json:"metadata,omitempty"`
}

// TaskQueryParams represents the parameters for querying a task.
type TaskQueryParams struct {
	ID            string         `json:"id"`
	HistoryLength *int           `json:"historyLength,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

func (p *TaskQueryParams) Validate() error {
	if p.ID == "" {
		return errors.New("id is required")
	}
	if p.HistoryLength != nil && *p.HistoryLength <= 0 {
		return errors.New("historyLength must be greater than 0")
	}
	return nil
}

// TaskIDParams represents the parameters for identifying a task.
type TaskIDParams struct {
	ID       string         `json:"id"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

func (p *TaskIDParams) Validate() error {
	if p.ID == "" {
		return errors.New("id is required")
	}
	if p.Metadata == nil {
		p.Metadata = make(map[string]any)
	}
	return nil
}

// Validate checks if the required fields in TaskSendParams are set correctly.
func (p *TaskSendParams) Validate() error {
	if p.ID == "" {
		return errors.New("id is required")
	}
	if err := p.Message.Validate(); err != nil {
		return err
	}
	if p.Metadata == nil {
		p.Metadata = make(map[string]any)
	}
	if p.PushNotification != nil {
		if err := p.PushNotification.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// TaskState represents the possible states of a task.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateCanceled      TaskState = "canceled"
	TaskStateFailed        TaskState = "failed"
	TaskStateUnknown       TaskState = "unknown"
)

func (s TaskState) String() string {
	switch s {
	case TaskStateSubmitted:
		return "submitted"
	case TaskStateWorking:
		return "working"
	case TaskStateInputRequired:
		return "input-required"
	case TaskStateCompleted:
		return "completed"
	case TaskStateCanceled:
		return "canceled"
	case TaskStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

func (s TaskState) Final() bool {
	switch s {
	case TaskStateCompleted, TaskStateCanceled, TaskStateFailed, TaskStateInputRequired:
		return true
	default:
		return false
	}
}

// Artifact represents an artifact created by the agent.
type Artifact struct {
	Name        *string        `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Parts       []Part         `json:"parts"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Index       int            `json:"index"`
	Append      *bool          `json:"append,omitempty"`
	LastChunk   *bool          `json:"lastChunk,omitempty"`
}

// Validate checks if the required fields in Artifact are set correctly.
func (a *Artifact) Validate() error {
	if len(a.Parts) == 0 {
		return errors.New("parts must have at least one entry")
	}
	for _, part := range a.Parts {
		if err := part.Validate(); err != nil {
			return fmt.Errorf("part validation failed: %w", err)
		}
	}
	if a.Index < 0 {
		return errors.New("index must be non-negative")
	}
	// Metadata, Append, and LastChunk are optional
	return nil
}

// Message represents a message exchanged between the user and the agent.
type Message struct {
	Role     MessageRole    `json:"role"`
	Parts    []Part         `json:"parts"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks if the required fields in Message are set correctly.
func (m *Message) Validate() error {
	if m.Role != MessageRoleUser && m.Role != MessageRoleAgent {
		return errors.New("invalid role, must be 'user' or 'agent'")
	}
	if len(m.Parts) == 0 {
		return errors.New("parts must have at least one entry")
	}
	for _, part := range m.Parts {
		if err := part.Validate(); err != nil {
			return err
		}
	}
	if m.Metadata == nil {
		m.Metadata = make(map[string]any)
	}
	return nil
}

// MessageRole represents the role of the message sender.
type MessageRole string

const (
	MessageRoleUser  MessageRole = "user"
	MessageRoleAgent MessageRole = "agent"
)

// Part represents a part of a message or artifact.
type Part struct {
	Type     PartType       `json:"type"`
	Text     *string        `json:"text,omitempty"` // For TextPart
	File     *FileContent   `json:"file,omitempty"` // For FilePart
	Data     map[string]any `json:"data,omitempty"` // For DataPart
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Validate checks if the required fields in Part are set correctly.
func (p *Part) Validate() error {
	if p.Type == "" {
		return errors.New("type is required")
	}

	switch p.Type {
	case PartTypeText:
		if p.Text == nil {
			return errors.New("text is required for PartTypeText")
		}
	case PartTypeFile:
		if p.File == nil {
			return errors.New("file is required for PartTypeFile")
		}
		if p.File.Bytes == nil && p.File.URI == nil {
			return errors.New("either bytes or uri is required for PartTypeFile")
		}
	case PartTypeData:
		if p.Data == nil {
			return errors.New("data is required for PartTypeData")
		}
	default:
		return errors.New("invalid part type")
	}
	return nil
}

func TextPart(text string) Part {
	return Part{
		Type: PartTypeText,
		Text: Ptr(text),
	}
}

func BytesFilePart(name, mimeType string, bytes []byte) Part {
	return Part{
		Type: PartTypeFile,
		File: &FileContent{
			Name:     Ptr(name),
			MimeType: Ptr(mimeType),
			Bytes:    Ptr(base64.StdEncoding.EncodeToString(bytes)),
		},
	}
}

func URIFilePart(name, mimeType, uri string) Part {
	return Part{
		Type: PartTypeFile,
		File: &FileContent{
			Name:     Ptr(name),
			MimeType: Ptr(mimeType),
			URI:      Ptr(uri),
		},
	}
}

func DataPart(data map[string]any) Part {
	return Part{
		Type: PartTypeData,
		Data: data,
	}
}

// PartType represents the type of a part.
type PartType string

const (
	PartTypeText PartType = "text"
	PartTypeFile PartType = "file"
	PartTypeData PartType = "data"
)

// FileContent represents the file information in a FileContent.
type FileContent struct {
	Name     *string `json:"name,omitempty"`
	MimeType *string `json:"mimeType,omitempty"`
	Bytes    *string `json:"bytes,omitempty"` // Base64 encoded content
	URI      *string `json:"uri,omitempty"`
}

// PushNotificationConfig represents the configuration for push notifications.
type PushNotificationConfig struct {
	URL            string          `json:"url"`
	Token          *string         `json:"token,omitempty"`
	Authentication *Authentication `json:"authentication,omitempty"`
}

// Validate checks if the required fields in PushNotificationConfig are set correctly.
func (p *PushNotificationConfig) Validate() error {
	if p.URL == "" {
		return errors.New("url is required")
	}
	if p.Authentication != nil {
		if len(p.Authentication.Schemes) == 0 {
			return errors.New("authentication schemes must have at least one entry")
		}
	}
	return nil
}

// TaskPushNotificationConfig represents the push notification configuration for a specific task.
type TaskPushNotificationConfig struct {
	ID                     string                 `json:"id"`
	PushNotificationConfig PushNotificationConfig `json:"pushNotificationConfig"`
	Metadata               map[string]any         `json:"metadata,omitempty"`
}

// Validate checks if the required fields in TaskPushNotificationConfig are set correctly.
func (t *TaskPushNotificationConfig) Validate() error {
	if t.ID == "" {
		return errors.New("id is required")
	}
	if err := t.PushNotificationConfig.Validate(); err != nil {
		return err
	}
	if t.Metadata == nil {
		t.Metadata = make(map[string]any)
	}
	return nil
}
