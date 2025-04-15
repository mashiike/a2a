package a2a

import (
	"bufio"
	"bytes"
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

	"github.com/google/uuid"
	"github.com/mashiike/a2a/jsonrpc"
)

// AgentCardResolver is a struct for resolving agent cards.
// It uses an HTTP client and a specified path.
type AgentCardResolver struct {
	HTTPClient *http.Client // HTTP client
	Path       string       // Path to the agent card
}

// DefaultAgentCardPath is the default path for the agent card.
const DefaultAgentCardPath = "/.well-known/agent.json"

// DefaultAgentCardResolver is the default instance of AgentCardResolver.
var DefaultAgentCardResolver = &AgentCardResolver{}

// Resolve retrieves the agent card based on the specified URL.
func (r *AgentCardResolver) Resolve(ctx context.Context, base *url.URL) (*AgentCard, error) {
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	path := r.Path
	if path == "" {
		path = DefaultAgentCardPath
	}
	u := base.JoinPath(path)

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to resolve agent card: %s", resp.Status)
	}
	var card AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, err
	}
	return &card, nil
}

// Resolve uses the default AgentCardResolver to resolve the agent card.
func Resolve(ctx context.Context, base *url.URL) (*AgentCard, error) {
	return DefaultAgentCardResolver.Resolve(ctx, base)
}

// AuthProvider is an interface for authenticating requests.
type AuthProvider interface {
	Authorize(card *AgentCard, req *http.Request) error
}

// AuthProviderFunc is a type that implements the AuthProvider interface as a function.
type AuthProviderFunc func(card *AgentCard, req *http.Request) error

// Authorize authenticates a request.
func (f AuthProviderFunc) Authorize(card *AgentCard, req *http.Request) error {
	return f(card, req)
}

// RequestIDGenerator is an interface for generating request IDs.
type RequestIDGenerator interface {
	Generate(context.Context) *jsonrpc.RequestID
}

// RequestIDGeneratorFunc is a type that implements the RequestIDGenerator interface as a function.
type RequestIDGeneratorFunc func(context.Context) *jsonrpc.RequestID

// Generate generates a request ID.
func (f RequestIDGeneratorFunc) Generate(ctx context.Context) *jsonrpc.RequestID {
	return f(ctx)
}

// Client is a client for communicating with the agent.
type Client struct {
	AgentCard          *AgentCard         // Agent card
	HTTPClient         *http.Client       // HTTP client
	AuthProvider       AuthProvider       // Authentication provider
	Logger             *slog.Logger       // Logger
	RequestIDGenerator RequestIDGenerator // Request ID generator

	agentURL   *url.URL           // Agent URL
	mu         sync.Mutex         // Mutex for synchronization
	wg         sync.WaitGroup     // Wait group for goroutines
	baseCtx    context.Context    // Base context
	baseCancel context.CancelFunc // Cancel function for the base context
}

// NewClient creates a new Client.
func NewClient(rawURL string) (*Client, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	card, err := Resolve(context.Background(), parsedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve agent card: %w", err)
	}
	return &Client{
		AgentCard: card,
	}, nil
}

// logger retrieves the client's logger. It uses the default logger if none is set.
func (c *Client) logger() *slog.Logger {
	if c.Logger == nil {
		return slog.Default()
	}
	return c.Logger
}

// do sends a request and processes the response.
func (c *Client) do(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, int, error) {
	resp, err := c.send(ctx, req, false)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	var rpcResp jsonrpc.Response
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, resp.StatusCode, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: resp.StatusCode,
			Message:        fmt.Errorf("failed to decode response: %w", err),
		}
	}
	if req.ID.String() != rpcResp.ID.String() {
		c.logger().WarnContext(ctx, "request ID does not match response ID", "requestID", req.ID.String(), "responseID", rpcResp.ID.String())
	}
	if rpcResp.Error != nil {
		return nil, resp.StatusCode, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: resp.StatusCode,
			Message:        fmt.Errorf("error in response: %s", rpcResp.Error.Message),
		}
	}
	c.logger().DebugContext(ctx, "do", "response", rpcResp)
	return &rpcResp, resp.StatusCode, nil
}

// clientContext retrieves the client's base context.
func (c *Client) clientContext() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseCtx == nil {
		c.baseCtx, c.baseCancel = context.WithCancel(context.Background())
	}
	return c.baseCtx
}

// doStreaming processes a streaming request.
func (c *Client) doStreaming(ctx context.Context, req *jsonrpc.Request, yaild func(*jsonrpc.Response, bool) bool) error {
	ctx, cancel := context.WithCancel(ctx)
	resp, err := c.send(ctx, req, true)
	if err != nil {
		cancel()
		return err
	}
	clientCtx := c.clientContext()
	select {
	case <-clientCtx.Done():
		resp.Body.Close()
		cancel()
		return errors.New("client is closed")
	default:
	}
	c.wg.Add(2)
	go func() {
		defer func() {
			resp.Body.Close()
			c.wg.Done()
		}()
		select {
		case <-clientCtx.Done():
			return
		case <-ctx.Done():
			return
		}
	}()
	go func() {
		defer func() {
			cancel()
			yaild(nil, true)
			c.wg.Done()
		}()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := strings.TrimPrefix(line, "data:")
			payload = strings.TrimSpace(payload)
			if payload == "" {
				continue
			}
			var rpcResp jsonrpc.Response
			if err := json.Unmarshal([]byte(payload), &rpcResp); err != nil {
				c.logger().WarnContext(ctx, "failed to unmarshal streaming response", "error", err)
				continue
			}
			if !yaild(&rpcResp, false) {
				return
			}
		}
	}()
	return nil
}

// send sends an HTTP request.
func (c *Client) send(ctx context.Context, req *jsonrpc.Request, isStraming bool) (*http.Response, error) {
	if c.AgentCard == nil {
		return nil, fmt.Errorf("agent card is not set")
	}
	if c.RequestIDGenerator != nil {
		req.ID = c.RequestIDGenerator.Generate(ctx)
	} else {
		req.ID = &jsonrpc.RequestID{
			StringValue: uuid.NewString(),
			IsInt:       false,
		}
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(req); err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.AgentCard.URL, &body)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if isStraming {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}
	if c.AuthProvider != nil {
		if err := c.AuthProvider.Authorize(c.AgentCard, httpReq); err != nil {
			return nil, err
		}
	}
	c.logger().DebugContext(ctx, "sendRequest", "request", req)
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		bs, err := io.ReadAll(resp.Body)
		if err != nil {
			c.logger().WarnContext(ctx, "failed to read response body on not HTTP Status 200 Ok", "error", err)
			return nil, &jsonrpc.Error{
				ID:             req.ID,
				HTTPStatusCode: resp.StatusCode,
				Message:        errors.New("request failed"),
			}
		}
		var rpcResp jsonrpc.Response
		if err := json.Unmarshal(bs, &rpcResp); err == nil && rpcResp.Error != nil {
			return resp, &jsonrpc.Error{
				ID:             rpcResp.ID,
				HTTPStatusCode: resp.StatusCode,
				Message:        rpcResp.Error,
			}
		}
		return resp, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: resp.StatusCode,
			Message:        errors.New(string(bs)),
		}
	}
	return resp, nil
}

// SendTask sends a task.
func (c *Client) SendTask(ctx context.Context, params TaskSendParams) (*Task, error) {
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksSend, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, code, err := c.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("SendTask: %w", err)
	}
	var task Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: code,
			Message:        fmt.Errorf("failed to resulta unmarshal as task: %w", err),
		}
	}
	return &task, nil
}

// GetTask retrieves a task.
func (c *Client) GetTask(ctx context.Context, params TaskQueryParams) (*Task, error) {
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksGet, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, code, err := c.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetTask: %w", err)
	}
	var task Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: code,
			Message:        fmt.Errorf("failed to resulta unmarshal as task: %w", err),
		}
	}
	return &task, nil
}

// CancelTask cancels a task.
func (c *Client) CancelTask(ctx context.Context, params TaskIDParams) (*Task, error) {
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksCancel, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, code, err := c.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CancelTask: %w", err)
	}
	var task Task
	if err := json.Unmarshal(resp.Result, &task); err != nil {
		return nil, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: code,
			Message:        fmt.Errorf("failed to resulta unmarshal as task: %w", err),
		}
	}
	return &task, nil
}

// SetPushNotification updates the push notification settings.
func (c *Client) SetPushNotification(ctx context.Context, params *TaskPushNotificationConfig) (*TaskPushNotificationConfig, error) {
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksPushNotificationSet, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, code, err := c.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("SetPushNotification: %w", err)
	}
	var config TaskPushNotificationConfig
	if err := json.Unmarshal(resp.Result, &config); err != nil {
		return nil, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: code,
			Message:        fmt.Errorf("failed to resulta unmarshal as task push notification config: %w", err),
		}
	}
	return &config, nil
}

// GetPushNotification retrieves the push notification settings.
func (c *Client) GetPushNotification(ctx context.Context, params TaskIDParams) (*TaskPushNotificationConfig, error) {
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksPushNotificationGet, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	resp, code, err := c.do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("GetPushNotification: %w", err)
	}
	var config TaskPushNotificationConfig
	if err := json.Unmarshal(resp.Result, &config); err != nil {
		return nil, &jsonrpc.Error{
			ID:             req.ID,
			HTTPStatusCode: code,
			Message:        fmt.Errorf("failed to resulta unmarshal as task push notification config: %w", err),
		}
	}
	return &config, nil
}

// SendTaskSubscribe subscribes to streaming events for a task.
func (c *Client) SendTaskSubscribe(ctx context.Context, params TaskSendParams) (<-chan StreamingEvent, error) {
	if c.AgentCard.Capabilities.Streaming == nil || !*c.AgentCard.Capabilities.Streaming {
		return nil, ErrStreaminNotSupported
	}
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksSendSubscribe, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	streamingEvents := make(chan StreamingEvent)
	err = c.doStreaming(ctx, req, func(resp *jsonrpc.Response, isDone bool) bool {
		if isDone {
			close(streamingEvents)
			return false
		}
		var event StreamingEvent
		if err := json.Unmarshal(resp.Result, &event); err != nil {
			c.logger().WarnContext(ctx, "failed to unmarshal streaming event", "error", err, "result", string(resp.Result))
			return true
		}
		streamingEvents <- event
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("SendTaskSubscribe: %w", err)
	}
	return streamingEvents, nil
}

// ResubscribeTask resubscribes to streaming events for a task.
func (c *Client) ResubscribeTask(ctx context.Context, params TaskIDParams) (<-chan StreamingEvent, error) {
	if c.AgentCard.Capabilities.Streaming == nil || !*c.AgentCard.Capabilities.Streaming {
		return nil, ErrStreaminNotSupported
	}
	req, err := jsonrpc.NewRequest(jsonrpc.MethodTasksResubscribe, params)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	streamingEvents := make(chan StreamingEvent)
	err = c.doStreaming(ctx, req, func(resp *jsonrpc.Response, isDone bool) bool {
		if isDone {
			close(streamingEvents)
			return false
		}
		var event StreamingEvent
		if err := json.Unmarshal(resp.Result, &event); err != nil {
			c.logger().WarnContext(ctx, "failed to unmarshal streaming event", "error", err, "result", string(resp.Result))
			return true
		}
		streamingEvents <- event
		return true
	})
	if err != nil {
		return nil, fmt.Errorf("ResubscribeTask: %w", err)
	}
	return streamingEvents, nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.baseCancel != nil {
		c.baseCancel()
	}
	c.wg.Wait()
	c.HTTPClient = nil
	c.AgentCard = nil
	c.AuthProvider = nil
	c.RequestIDGenerator = nil
}
