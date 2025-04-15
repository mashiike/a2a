package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mashiike/a2a/jsonrpc"
)

// Agent is an interface for processing tasks.
type Agent interface {
	Invoke(ctx context.Context, r TaskResponder, task *Task) error
}

// AgentFunc is a type that implements the Agent interface as a function.
type AgentFunc func(ctx context.Context, r TaskResponder, task *Task) error

// Invoke calls the AgentFunc.
func (f AgentFunc) Invoke(ctx context.Context, r TaskResponder, task *Task) error {
	return f(ctx, r, task)
}

// Handler is a struct for handling JSON-RPC requests.
type Handler struct {
	card                       *AgentCard
	agent                      Agent
	baseURL                    *url.URL
	notFoundHandler            http.Handler
	methodNotAllowedHandler    http.Handler
	mux                        *http.ServeMux
	logger                     *slog.Logger
	sessIDGenerator            func(*http.Request) string
	store                      Store
	queue                      PubSub
	notificationURLVerifier    func(context.Context, *TaskPushNotificationConfig) error
	notificationRequestBuilder func(context.Context, *TaskPushNotificationConfig, *Task) (*http.Request, error)
	notificationHTTPClient     *http.Client
	taskStatePollingInterval   time.Duration
	agentHistoryLength         int

	extraRPCHandlersMux sync.Mutex
	extraRPCHandlers    map[string]func(http.ResponseWriter, *http.Request, *jsonrpc.Request)
}

// HandlerOptions defines configuration options for the Handler.
type HandlerOptions struct {
	// AgentCardPath is the path for the agent card.
	// Default is "/.well-known/agent.json".
	AgentCardPath string

	// Logger is the logger for the handler.
	// Default is slog.Default().
	Logger *slog.Logger

	// Middlewares is a list of middlewares applied to the JSON-RPC handler.
	// These middlewares are not applied to the agent card handler.
	Middlewares []func(http.Handler) http.Handler

	// NotFoundHandler is the handler for 404 Not Found.
	// Default is a JSON-RPC error response.
	NotFoundHandler http.Handler

	// MethodNotAllowedHandler is the handler for 405 Method Not Allowed.
	// Default is a JSON-RPC error response.
	MethodNotAllowedHandler http.Handler

	// SessionIDGenerator is a function to generate session IDs.
	// Default is a UUID-based generator.
	SessionIDGenerator func(*http.Request) string

	// Store is used to save and load task data.
	// Default is InMemoryStore.
	Store Store

	// TaskEventQueue is the queue for task events.
	// Default is ChannelPubSub.
	TaskEventQueue PubSub

	// PushNotificationURLVerifier is a function to verify push notification URLs.
	// Default is nil, meaning no verification is performed.
	PushNotificationURLVerifier func(context.Context, *TaskPushNotificationConfig) error

	// PushNotificationRequestBuilder is a function to build requests for push notifications.
	PushNotificationRequestBuilder func(context.Context, *TaskPushNotificationConfig, *Task) (*http.Request, error)

	// PushNotificationHTTPClient is the HTTP client for push notifications.
	// Default is http.DefaultClient.
	PushNotificationHTTPClient *http.Client

	// AgentHistoryLength is the length of the task history for the agent.
	// Default is -1, meaning all history is retained.
	AgentHistoryLength *int

	// TaskStatePollingInterval is the interval for polling task states.
	// Default is 5 seconds.
	TaskStatePollingInterval time.Duration
}

// fillDefaults sets default values for HandlerOptions if they are not provided.
func (options *HandlerOptions) fillDefaults() {
	if options == nil {
		options = &HandlerOptions{}
	}
	if options.AgentCardPath == "" {
		options.AgentCardPath = DefaultAgentCardPath
	}
	if options.Logger == nil {
		options.Logger = slog.Default().With("component", "github.com/mashiike/a2a.Handler")
	}
	if options.NotFoundHandler == nil {
		options.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jsonrpc.WriteError(w, nil, &jsonrpc.ErrorMessage{
				Code:    jsonrpc.CodeMethodNotFound,
				Message: jsonrpc.CodeMessage(jsonrpc.CodeMethodNotFound),
			}, http.StatusNotFound)
		})
	}
	if options.MethodNotAllowedHandler == nil {
		options.MethodNotAllowedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jsonrpc.WriteError(w, nil, &jsonrpc.ErrorMessage{
				Code:    jsonrpc.CodeMethodNotFound,
				Message: jsonrpc.CodeMessage(jsonrpc.CodeMethodNotFound),
			}, http.StatusMethodNotAllowed)
		})
	}
	if options.Middlewares == nil {
		options.Middlewares = []func(http.Handler) http.Handler{}
	}
	if options.SessionIDGenerator == nil {
		options.SessionIDGenerator = func(r *http.Request) string {
			return uuid.New().String()
		}
	}
	if options.AgentHistoryLength == nil {
		options.AgentHistoryLength = new(int)
		*options.AgentHistoryLength = -1
	}
	if options.TaskStatePollingInterval == 0 {
		options.TaskStatePollingInterval = 5 * time.Second
	}
	if options.Store == nil {
		options.Store = &InMemoryStore{}
	}
	if options.TaskEventQueue == nil {
		options.TaskEventQueue = &ChannelPubSub{}
	}
	if options.PushNotificationRequestBuilder == nil {
		options.PushNotificationRequestBuilder = DefaultPushNotificationRequestBuilder
	}
	if options.PushNotificationURLVerifier == nil {
		options.PushNotificationURLVerifier = func(context.Context, *TaskPushNotificationConfig) error {
			// no verification
			return nil
		}
	}
	if options.PushNotificationHTTPClient == nil {
		options.PushNotificationHTTPClient = http.DefaultClient
	}
}

// validateCapabilities validates the capabilities of the agent card based on the provided options.
func (options *HandlerOptions) validateCapabilities(card *AgentCard) error {
	_, implementedPushStore := options.Store.(PushNotificationStore)
	if card.Capabilities.PushNotifications != nil {
		if !*card.Capabilities.PushNotifications && implementedPushStore {
			return fmt.Errorf("agent card push notification is enabled but store does not implement PushNotificationStore")
		}
	} else {
		card.Capabilities.PushNotifications = new(bool)
		*card.Capabilities.PushNotifications = implementedPushStore
	}
	if card.Capabilities.Streaming != nil {
		if *card.Capabilities.Streaming && options.TaskEventQueue == nil {
			return fmt.Errorf("agent card streaming is enabled but TaskEventQueue is not set")
		}
	} else {
		card.Capabilities.Streaming = new(bool)
		*card.Capabilities.Streaming = (options.TaskEventQueue != nil)
	}
	if card.Capabilities.StateTransitionHistory == nil {
		card.Capabilities.StateTransitionHistory = new(bool)
		*card.Capabilities.StateTransitionHistory = true
	}
	return nil
}

// NewHandler creates a new Handler instance.
func NewHandler(card *AgentCard, agent Agent, options *HandlerOptions) (*Handler, error) {
	if options == nil {
		options = &HandlerOptions{}
	}
	options.fillDefaults()
	h := &Handler{
		card:                       card,
		agent:                      agent,
		mux:                        http.NewServeMux(),
		notFoundHandler:            options.NotFoundHandler,
		methodNotAllowedHandler:    options.MethodNotAllowedHandler,
		logger:                     options.Logger,
		sessIDGenerator:            options.SessionIDGenerator,
		store:                      options.Store,
		queue:                      options.TaskEventQueue,
		notificationURLVerifier:    options.PushNotificationURLVerifier,
		notificationRequestBuilder: options.PushNotificationRequestBuilder,
		notificationHTTPClient:     options.PushNotificationHTTPClient,
		agentHistoryLength:         *options.AgentHistoryLength,
		taskStatePollingInterval:   options.TaskStatePollingInterval,
		extraRPCHandlers:           make(map[string]func(http.ResponseWriter, *http.Request, *jsonrpc.Request)),
	}
	if card == nil {
		return nil, ErrAgentCardRequired
	}
	if err := options.validateCapabilities(card); err != nil {
		return nil, err
	}
	if err := card.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(card.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid agent card URL: %w", err)
	}
	h.baseURL = baseURL
	h.mux.HandleFunc(options.AgentCardPath, h.handleAgentCard)
	rpcHandler := http.Handler(http.HandlerFunc(h.handleRPC))
	for _, middleware := range options.Middlewares {
		rpcHandler = middleware(rpcHandler)
	}
	path := baseURL.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	h.mux.Handle(path, rpcHandler)
	return h, nil
}

// ServeHTTP processes HTTP requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.logger.DebugContext(r.Context(), "ServeHTTP called", "method", r.Method, "url", r.URL.String())
	handler, pattern := h.mux.Handler(r)
	if pattern == "" || handler == nil {
		h.logger.DebugContext(r.Context(), "Handler not found", "url", r.URL.String())
		h.notFoundHandler.ServeHTTP(w, r)
		return
	}
	h.logger.DebugContext(r.Context(), "Handler found", "pattern", pattern)
	handler.ServeHTTP(w, r)
}

// handleAgentCard processes requests for the agent card.
func (h *Handler) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	h.logger.DebugContext(r.Context(), "handleAgentCard called", "method", r.Method, "url", r.URL.String())
	if r.Method != http.MethodGet {
		h.logger.DebugContext(r.Context(), "Method not allowed", "method", r.Method)
		h.methodNotAllowedHandler.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(h.card); err != nil {
		h.logger.WarnContext(r.Context(), "Failed to encode agent card", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// HandleRPC registers a custom JSON-RPC method.
func (h *Handler) HandleRPC(method string, handler func(http.ResponseWriter, *http.Request, *jsonrpc.Request)) error {
	h.extraRPCHandlersMux.Lock()
	defer h.extraRPCHandlersMux.Unlock()
	reserved := jsonrpc.Methods()
	for _, m := range reserved {
		if m == method {
			return fmt.Errorf("method %s is reserved", method)
		}
	}
	if _, ok := h.extraRPCHandlers[method]; ok {
		return fmt.Errorf("method %s already registered", method)
	}
	h.extraRPCHandlers[method] = handler
	return nil
}

func (h *Handler) rpcHandler(method string) (func(http.ResponseWriter, *http.Request, *jsonrpc.Request), bool) {
	h.extraRPCHandlersMux.Lock()
	defer h.extraRPCHandlersMux.Unlock()
	handler, ok := h.extraRPCHandlers[method]
	if ok {
		return handler, true
	}
	return nil, false
}

func (h *Handler) handleRPC(w http.ResponseWriter, r *http.Request) {
	h.logger.DebugContext(r.Context(), "handleRPC called", "method", r.Method, "url", r.URL.String())
	if r.Method != http.MethodPost {
		h.logger.DebugContext(r.Context(), "Method not allowed", "method", r.Method)
		h.methodNotAllowedHandler.ServeHTTP(w, r)
		return
	}
	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.DebugContext(r.Context(), "Failed to parse JSON-RPC request", "error", err)
		jsonrpc.WriteError(w, nil, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeParseError,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeParseError),
		}, http.StatusBadRequest)
		return
	}
	validationDiagnostic := make(map[string]any)
	if req.JSONRPC != "2.0" {
		validationDiagnostic["jsonrpc"] = errors.New("must JSON-RPC Version is 2.0")
	}
	if req.Method == "" {
		validationDiagnostic["method"] = errors.New("method is required")
	}
	if req.Params == nil {
		validationDiagnostic["params"] = errors.New("params is required")
	}
	if len(validationDiagnostic) > 0 {
		h.logger.DebugContext(r.Context(), "Invalid JSON-RPC request", "diagnostic", validationDiagnostic)
		jsonrpc.WriteError(w, &req, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeInvalidRequest,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeInvalidRequest),
			Data:    validationDiagnostic,
		}, http.StatusBadRequest)
		return
	}
	h.logger.DebugContext(r.Context(), "JSON-RPC method invoked", "method", req.Method)
	switch req.Method {
	case jsonrpc.MethodTasksSend:
		h.onSendTask(w, r, &req)
		return
	case jsonrpc.MethodTasksGet:
		h.onGetTask(w, r, &req)
		return
	case jsonrpc.MethodTasksCancel:
		h.onCancelTask(w, r, &req)
		return
	case jsonrpc.MethodTasksPushNotificationSet:
		h.onSetPushNotification(w, r, &req)
		return
	case jsonrpc.MethodTasksPushNotificationGet:
		h.onGetPushNotification(w, r, &req)
		return
	case jsonrpc.MethodTasksResubscribe:
		h.onResubscribe(w, r, &req)
		return
	case jsonrpc.MethodTasksSendSubscribe:
		h.onSendTaskSubscribe(w, r, &req)
		return
	default:
		if rpcHandler, ok := h.rpcHandler(req.Method); ok {
			h.logger.DebugContext(r.Context(), "Custom JSON-RPC method invoked", "method", req.Method)
			rpcHandler(w, r, &req)
			return
		}
		h.logger.DebugContext(r.Context(), "JSON-RPC method not found", "method", req.Method)
		jsonrpc.WriteError(w, &req, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeMethodNotFound,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeMethodNotFound),
			Data:    map[string]any{"method": req.Method},
		}, http.StatusNotFound)
	}
}

func (h *Handler) onSendTask(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onSendTask called", "task_id", rpcReq.ID)
	params, err := h.parseTaskSendParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse task send params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(httpReq.Context())
	defer cancel()
	if err := h.processTask(ctx, httpReq, rpcReq, params); err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to process task", "error", err, "task_id", params.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusInternalServerError)
		return
	}
	h.logger.DebugContext(httpReq.Context(), "Task processed successfully", "task_id", params.ID)
	h.processGetTask(w, httpReq, rpcReq, TaskQueryParams{
		ID:            params.ID,
		HistoryLength: params.HistoryLength,
		Metadata:      params.Metadata,
	})
}

func (h *Handler) onGetTask(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onGetTask called", "task_id", rpcReq.ID)
	params, err := h.parseTaskQueryParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse task query params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	h.processGetTask(w, httpReq, rpcReq, params)
}

func (h *Handler) processGetTask(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request, params TaskQueryParams) {
	h.logger.DebugContext(httpReq.Context(), "processGetTask called", "task_id", params.ID)
	task, err := h.store.GetTask(httpReq.Context(), params.ID)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to get task", "error", err, "task_id", params.ID)
		h.handleTaskError(w, rpcReq, params.ID, err)
		return
	}
	if params.HistoryLength != nil && *params.HistoryLength > 0 {
		history, err := h.store.GetHistory(httpReq.Context(), task.SessionID, *params.HistoryLength)
		if err != nil {
			h.logger.WarnContext(httpReq.Context(), "Failed to get task history", "error", err, "task_id", params.ID)
			h.handleTaskError(w, rpcReq, params.ID, err)
			return
		}
		task.History = history
	}
	h.logger.DebugContext(httpReq.Context(), "Task retrieved successfully", "task_id", params.ID)
	jsonrpc.WriteResult(w, rpcReq, task, http.StatusOK)
}

func (h *Handler) onCancelTask(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onCancelTask called", "task_id", rpcReq.ID)
	params, err := h.parseTaskIDParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse task ID params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	tr := h.NewTaskResponder(params.ID)
	canceledState := TaskStatus{
		State: TaskStateCanceled,
	}
	if err := tr.SetStatus(httpReq.Context(), canceledState, true, params.Metadata); err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to cancel task", "error", err, "task_id", params.ID)
		h.handleTaskError(w, rpcReq, params.ID, err)
		return
	}
	h.logger.DebugContext(httpReq.Context(), "Task canceled successfully", "task_id", params.ID)
	h.processGetTask(w, httpReq, rpcReq, TaskQueryParams{
		ID:       params.ID,
		Metadata: params.Metadata,
	})
}

func (h *Handler) handleTaskError(w http.ResponseWriter, rpcReq *jsonrpc.Request, taskID string, err error) {
	if errors.Is(err, ErrTaskNotFound) {
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeTaskNotFound,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeTaskNotFound),
			Data:    map[string]any{"task_id": taskID},
		}, http.StatusNotFound)
		return
	}
	jsonrpc.WriteError(w, rpcReq, err, http.StatusInternalServerError)
}

func (h *Handler) parseTaskPushNotificationParams(params json.RawMessage) (*TaskPushNotificationConfig, error) {
	var pushNotificationConfig TaskPushNotificationConfig
	if err := json.Unmarshal(params, &pushNotificationConfig); err != nil {
		return nil, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeParseError,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeParseError),
		}
	}
	if err := pushNotificationConfig.Validate(); err != nil {
		return nil, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeInvalidParams,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeInvalidParams),
			Data:    map[string]any{"detail": err.Error()},
		}
	}
	return &pushNotificationConfig, nil
}

func (h *Handler) isPushNotificationSupported() bool {
	if h.card.Capabilities.PushNotifications == nil {
		return false
	}
	return *h.card.Capabilities.PushNotifications
}

func (h *Handler) onSetPushNotification(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onSetPushNotification called", "task_id", rpcReq.ID)
	params, err := h.parseTaskPushNotificationParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse push notification params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	if err := h.processSetPushNotification(httpReq.Context(), params); err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to set push notification", "error", err, "task_id", params.ID)
		h.handleTaskError(w, rpcReq, params.ID, err)
		return
	}
	h.logger.DebugContext(httpReq.Context(), "Push notification set successfully", "task_id", params.ID)
	jsonrpc.WriteResult(w, rpcReq, params, http.StatusOK)
}

func (h *Handler) processSetPushNotification(ctx context.Context, params *TaskPushNotificationConfig) error {
	if !h.isPushNotificationSupported() {
		return &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodePushNotSupported,
			Message: jsonrpc.CodeMessage(jsonrpc.CodePushNotSupported),
		}
	}
	store, ok := h.store.(PushNotificationStore)
	if !ok {
		return &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodePushNotSupported,
			Message: jsonrpc.CodeMessage(jsonrpc.CodePushNotSupported),
		}
	}
	if err := store.CreateTaskPushNotification(ctx, params); err != nil {
		return err
	}
	return nil
}

func (h *Handler) onGetPushNotification(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onGetPushNotification called", "task_id", rpcReq.ID)
	params, err := h.parseTaskIDParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse task ID params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	if !h.isPushNotificationSupported() {
		h.logger.WarnContext(httpReq.Context(), "Push notification not supported", "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodePushNotSupported,
			Message: jsonrpc.CodeMessage(jsonrpc.CodePushNotSupported),
		}, http.StatusBadRequest)
		return
	}
	store, ok := h.store.(PushNotificationStore)
	if !ok {
		h.logger.WarnContext(httpReq.Context(), "Push notification store not implemented", "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodePushNotSupported,
			Message: jsonrpc.CodeMessage(jsonrpc.CodePushNotSupported),
		}, http.StatusBadRequest)
		return
	}
	pushNotificationConfig, err := store.GetTaskPushNotification(httpReq.Context(), params.ID)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to get push notification", "error", err, "task_id", params.ID)
		h.handleTaskError(w, rpcReq, params.ID, err)
		return
	}
	h.logger.DebugContext(httpReq.Context(), "Push notification retrieved successfully", "task_id", params.ID)
	jsonrpc.WriteResult(w, rpcReq, pushNotificationConfig, http.StatusOK)
}

func (h *Handler) isStreamingSupported() bool {
	if h.card.Capabilities.Streaming == nil {
		return false
	}
	return *h.card.Capabilities.Streaming
}

func (h *Handler) onResubscribe(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onResubscribe called", "task_id", rpcReq.ID)
	params, err := h.parseTaskQueryParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse task query params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	if !h.isStreamingSupported() {
		h.logger.WarnContext(httpReq.Context(), "Streaming not supported", "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeUnsupportedOperation,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeUnsupportedOperation),
		}, http.StatusBadRequest)
		return
	}
	if h.queue == nil {
		h.logger.WarnContext(httpReq.Context(), "Task event queue not set", "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeUnsupportedOperation,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeUnsupportedOperation),
		}, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(httpReq.Context())
	defer cancel()
	eventCh, err := h.queue.Subscribe(ctx, params.ID)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to subscribe to task events", "error", err, "task_id", params.ID)
		h.handleTaskError(w, rpcReq, params.ID, err)
		return
	}
	h.logger.DebugContext(httpReq.Context(), "Subscribed to task events", "task_id", params.ID)
	h.writeSSE(ctx, w, rpcReq, func() (StreamingEvent, error) {
		select {
		case <-ctx.Done():
			return StreamingEvent{}, context.Canceled
		case event, ok := <-eventCh:
			if !ok {
				return StreamingEvent{}, io.EOF
			}
			return event, nil
		}
	})
}

func (h *Handler) writeSSE(ctx context.Context, w http.ResponseWriter, rpcReq *jsonrpc.Request, fetcher func() (StreamingEvent, error)) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.logger.WarnContext(ctx, "Response writer does not support flushing")
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeUnsupportedOperation,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeUnsupportedOperation),
		}, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for {
		rpcResp := &jsonrpc.Response{
			JSONRPC: "2.0",
			ID:      rpcReq.ID,
		}
		select {
		case <-ctx.Done():
			h.logger.DebugContext(ctx, "Context canceled during SSE")
			return
		default:
		}
		event, fetchErr := fetcher()
		if fetchErr != nil {
			h.logger.WarnContext(ctx, "Failed to fetch streaming event", "error", fetchErr)
			if errors.Is(fetchErr, context.Canceled) ||
				errors.Is(fetchErr, context.DeadlineExceeded) ||
				errors.Is(fetchErr, io.EOF) {
				return
			}
			var rpcErr *jsonrpc.ErrorMessage
			if errors.As(fetchErr, &rpcErr) {
				rpcResp.Error = rpcErr
			} else {
				rpcResp.Error = &jsonrpc.ErrorMessage{
					Code:    jsonrpc.CodeInternalError,
					Message: jsonrpc.CodeMessage(jsonrpc.CodeInternalError),
				}
			}
		} else {
			bs, err := json.Marshal(&event)
			if err != nil {
				h.logger.WarnContext(ctx, "Failed to marshal streaming event", "error", err)
				continue
			}
			rpcResp.Result = bs
		}
		bs, err := json.Marshal(rpcResp)
		if err != nil {
			h.logger.WarnContext(ctx, "Failed to marshal JSON-RPC response", "error", err)
			continue
		}
		line := fmt.Sprintf("data: %s\n\n", string(bs))
		io.WriteString(w, line)
		flusher.Flush()
		if fetchErr != nil {
			return
		}
	}
}

func (h *Handler) onSendTaskSubscribe(w http.ResponseWriter, httpReq *http.Request, rpcReq *jsonrpc.Request) {
	h.logger.DebugContext(httpReq.Context(), "onSendTaskSubscribe called", "task_id", rpcReq.ID)
	params, err := h.parseTaskSendParams(rpcReq.Params)
	if err != nil {
		h.logger.WarnContext(httpReq.Context(), "Failed to parse task send params", "error", err, "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, err, http.StatusBadRequest)
		return
	}
	if !h.isStreamingSupported() {
		h.logger.WarnContext(httpReq.Context(), "Streaming not supported", "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeUnsupportedOperation,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeUnsupportedOperation),
		}, http.StatusBadRequest)
		return
	}
	if h.queue == nil {
		h.logger.WarnContext(httpReq.Context(), "Task event queue not set", "task_id", rpcReq.ID)
		jsonrpc.WriteError(w, rpcReq, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeUnsupportedOperation,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeUnsupportedOperation),
		}, http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithCancel(httpReq.Context())
	defer cancel()
	errCh := make(chan error, 1)
	defer close(errCh)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		h.logger.DebugContext(ctx, "start processing task", "task_id", params.ID)
		defer func() {
			h.logger.DebugContext(ctx, "finish processing task", "task_id", params.ID)
			wg.Done()
		}()
		if err := h.processTask(ctx, httpReq, rpcReq, params); err != nil {
			h.logger.WarnContext(ctx, "failed to process task", "error", err, "task_id", params.ID)
			errCh <- err
		}
	}()
	var eventCh <-chan StreamingEvent
	for {
		select {
		case <-ctx.Done():
			h.logger.DebugContext(ctx, "Context canceled during task subscription", "task_id", params.ID)
			h.handleTaskError(w, rpcReq, params.ID, context.Canceled)
			cancel()
			wg.Wait()
			return
		case err := <-errCh:
			if err == nil {
				continue
			}
			h.logger.WarnContext(ctx, "Error occurred during task processing", "error", err, "task_id", params.ID)
			h.handleTaskError(w, rpcReq, params.ID, err)
			cancel()
			wg.Wait()
			return
		default:
		}
		var err error
		eventCh, err = h.queue.Subscribe(ctx, params.ID)
		if err != nil {
			if errors.Is(err, ErrTaskNotFound) {
				h.logger.DebugContext(httpReq.Context(), "Task not found during subscription, retrying", "task_id", params.ID)
				time.Sleep(100 * time.Millisecond)
				continue
			}
			h.logger.WarnContext(httpReq.Context(), "Failed to subscribe to task events", "error", err, "task_id", params.ID)
			h.handleTaskError(w, rpcReq, params.ID, err)
			cancel()
			wg.Wait()
			return
		}
		break
	}
	h.logger.DebugContext(httpReq.Context(), "Subscribed to task events", "task_id", params.ID)
	h.writeSSE(httpReq.Context(), w, rpcReq, func() (StreamingEvent, error) {
		for {
			select {
			case <-ctx.Done():
				return StreamingEvent{}, context.Canceled
			case err := <-errCh:
				if err != nil {
					return StreamingEvent{}, err
				}
				return StreamingEvent{}, io.EOF
			case event, ok := <-eventCh:
				if !ok {
					return StreamingEvent{}, io.EOF
				}
				return event, nil
			}
		}
	})
	cancel()
	wg.Wait()
	h.logger.DebugContext(ctx, "onSendTaskSubscribe finished", "task_id", params.ID)
}

func (h *Handler) parseTaskSendParams(params json.RawMessage) (TaskSendParams, error) {
	var taskSendParams TaskSendParams
	if err := json.Unmarshal(params, &taskSendParams); err != nil {
		return TaskSendParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeParseError,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeParseError),
		}
	}
	if err := taskSendParams.Validate(); err != nil {
		return TaskSendParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeInvalidParams,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeInvalidParams),
			Data:    map[string]any{"detail": err.Error()},
		}
	}
	if taskSendParams.PushNotification != nil && !h.isPushNotificationSupported() {
		return TaskSendParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodePushNotSupported,
			Message: jsonrpc.CodeMessage(jsonrpc.CodePushNotSupported),
		}
	}
	return taskSendParams, nil
}

func (h *Handler) parseTaskQueryParams(params json.RawMessage) (TaskQueryParams, error) {
	var taskQueryParams TaskQueryParams
	if err := json.Unmarshal(params, &taskQueryParams); err != nil {
		return TaskQueryParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeParseError,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeParseError),
		}
	}
	if err := taskQueryParams.Validate(); err != nil {
		return TaskQueryParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeInvalidParams,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeInvalidParams),
			Data:    map[string]any{"detail": err.Error()},
		}
	}
	return taskQueryParams, nil
}

func (h *Handler) parseTaskIDParams(params json.RawMessage) (TaskIDParams, error) {
	var taskIDParams TaskIDParams
	if err := json.Unmarshal(params, &taskIDParams); err != nil {
		return TaskIDParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeParseError,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeParseError),
		}
	}
	if err := taskIDParams.Validate(); err != nil {
		return TaskIDParams{}, &jsonrpc.ErrorMessage{
			Code:    jsonrpc.CodeInvalidParams,
			Message: jsonrpc.CodeMessage(jsonrpc.CodeInvalidParams),
			Data:    map[string]any{"detail": err.Error()},
		}
	}
	return taskIDParams, nil
}

type contextKey struct{}

type rawRequest struct {
	httpReq *http.Request
	rpcReq  *jsonrpc.Request
}

// withRawRequet adds the raw HTTP request and JSON-RPC request to the context.
func withRawRequet(ctx context.Context, httpReq *http.Request, rpcReq *jsonrpc.Request) context.Context {
	return context.WithValue(ctx, contextKey{}, &rawRequest{
		httpReq: httpReq,
		rpcReq:  rpcReq,
	})
}

// FromContext returns the raw HTTP request and JSON-RPC request from the context.
// It returns nil if the context does not contain a raw request.
func FromContext(ctx context.Context) (*http.Request, *jsonrpc.Request) {
	rawReq, ok := ctx.Value(contextKey{}).(*rawRequest)
	if !ok {
		return nil, nil
	}
	return rawReq.httpReq, rawReq.rpcReq
}

func (h *Handler) processTask(ctx context.Context, httpReq *http.Request, rpcReq *jsonrpc.Request, params TaskSendParams) error {
	h.logger.DebugContext(ctx, "processTask called", "task_id", params.ID)
	if params.PushNotification != nil {
		if err := h.processSetPushNotification(ctx, &TaskPushNotificationConfig{
			ID:                     params.ID,
			PushNotificationConfig: *params.PushNotification,
			Metadata:               params.Metadata,
		}); err != nil {
			h.logger.WarnContext(ctx, "Failed to set push notification during task processing", "error", err, "task_id", params.ID)
			return err
		}
	}
	task := &Task{
		ID: params.ID,
		Status: TaskStatus{
			State: TaskStateSubmitted,
		},
		Metadata:  params.Metadata,
		Artifacts: make([]Artifact, 0),
		History:   make([]Message, 0),
	}
	if params.SessionID != nil {
		task.SessionID = *params.SessionID
	} else {
		task.SessionID = h.sessIDGenerator(httpReq)
	}
	if err := h.store.CreateTask(ctx, task); err != nil {
		h.logger.WarnContext(ctx, "Failed to create task", "error", err, "task_id", task.ID)
		return err
	}
	if params.SessionID != nil {
		history, err := h.store.GetHistory(ctx, *params.SessionID, h.agentHistoryLength)
		if err != nil {
			h.logger.WarnContext(ctx, "Failed to get task history during processing", "error", err, "task_id", task.ID)
			return err
		}
		task.History = history
	}
	task.History = append(task.History, params.Message)
	if err := h.store.AppendHistory(ctx, task.SessionID, params.Message); err != nil {
		h.logger.WarnContext(ctx, "Failed to append task history", "error", err, "task_id", task.ID)
		return err
	}
	ctx = withRawRequet(ctx, httpReq, rpcReq)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(h.taskStatePollingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-cctx.Done():
				h.logger.DebugContext(ctx, "Context canceled during task state polling", "task_id", task.ID)
				return
			case <-ticker.C:
				task, err := h.store.GetTask(cctx, task.ID)
				if err != nil {
					h.logger.WarnContext(ctx, "Failed to get task during state polling", "error", err, "task_id", task.ID)
					continue
				}
				if task.Status.State == TaskStateCanceled || task.Status.State == TaskStateFailed || task.Status.State == TaskStateCompleted {
					h.logger.DebugContext(ctx, "Task state polling finished", "task_id", task.ID, "state", task.Status.State)
					cancel()
					return
				}
			}
		}
	}()
	tr := h.NewTaskResponder(task.ID)
	if err := h.agent.Invoke(cctx, tr, task); err != nil {
		h.logger.WarnContext(ctx, "Failed to invoke agent", "error", err, "task_id", task.ID)
	}
	cancel()
	wg.Wait()
	h.logger.DebugContext(ctx, "Task processing finished", "task_id", task.ID)
	return nil
}
