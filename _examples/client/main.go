package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"os"

	"github.com/google/uuid"
	"github.com/mashiike/a2a"
)

func main() {
	var (
		agent     string
		logLevel  string
		sessionID string
		streaming bool
		history   int
	)
	flag.StringVar(&agent, "agent", "http://localhost:8080", "agent url")
	flag.StringVar(&logLevel, "log-level", "info", "log level")
	flag.StringVar(&sessionID, "session-id", "", "session id")
	flag.BoolVar(&streaming, "streaming", false, "enable streaming")
	flag.IntVar(&history, "history", 0, "history length")
	flag.Parse()
	var l slog.Level
	if err := l.UnmarshalText([]byte(logLevel)); err != nil {
		slog.Error("failed to parse log level", "error", err)
		os.Exit(1)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: l,
	})))

	client, err := a2a.NewClient(agent)
	if err != nil {
		slog.Error("failed to create client", "error", err)
		os.Exit(1)
	}
	defer client.Close()
	slog.Info("client created", "agent_card", client.AgentCard)
	if flag.NArg() == 0 {
		slog.Error("please specify a input message")
		os.Exit(1)
	}
	ctx := context.Background()
	params := a2a.TaskSendParams{
		ID: uuid.New().String(),
		Message: a2a.Message{
			Role: a2a.MessageRoleUser,
			Parts: []a2a.Part{
				a2a.TextPart(flag.Arg(0)),
			},
		},
	}
	if sessionID != "" {
		params.SessionID = a2a.Ptr(sessionID)
	}
	if history > 0 {
		params.HistoryLength = a2a.Ptr(history)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(params)

	// Example usage of the client
	if streaming {
		sendTaskSubscribe(ctx, client, params)
	} else {
		sendTask(ctx, client, params)
	}
}

func sendTask(ctx context.Context, client *a2a.Client, params a2a.TaskSendParams) {
	task, err := client.SendTask(ctx, params)
	if err != nil {
		slog.Error("failed to send task", "error", err)
		os.Exit(1)
	}
	for task.Status.State != a2a.TaskStateCompleted {
		slog.Info("task status", "status", task.Status)
		task, err = client.GetTask(ctx, a2a.TaskQueryParams{
			ID: params.ID,
		})
		if err != nil {
			slog.Error("failed to get task", "error", err)
			os.Exit(1)
		}
		if task.Status.State == a2a.TaskStateFailed {
			slog.Error("task failed", "status", task.Status)
			os.Exit(1)
		}
		if task.Status.State == a2a.TaskStateCanceled {
			slog.Error("task cancelled", "status", task.Status)
			os.Exit(1)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(task); err != nil {
		slog.Error("failed to encode task", "error", err)
		os.Exit(1)
	}
}

func sendTaskSubscribe(ctx context.Context, client *a2a.Client, params a2a.TaskSendParams) {
	eventCh, err := client.SendTaskSubscribe(ctx, params)
	if err != nil {
		slog.Error("failed to send task", "error", err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	for event := range eventCh {
		enc.Encode(event)
		if event.EventType() == "status_updated" {
			if event.StatusUpdated.Status.State == a2a.TaskStateCompleted {
				break
			}
			if event.StatusUpdated.Status.State == a2a.TaskStateFailed {
				slog.Error("task failed", "status", event.StatusUpdated.Status)
				os.Exit(1)
			}
			if event.StatusUpdated.Status.State == a2a.TaskStateCanceled {
				slog.Error("task cancelled", "status", event.StatusUpdated.Status)
				os.Exit(1)
			}
		}
	}
	task, err := client.GetTask(ctx, a2a.TaskQueryParams{
		ID: params.ID,
	})
	if err != nil {
		slog.Error("failed to get task", "error", err)
		os.Exit(1)
	}
	enc.Encode(task)
}
