# Agent2Agent (A2A) Go Library

[![GoDoc](https://godoc.org/github.com/mashiike/a2a?status.svg)](https://godoc.org/github.com/mashiike/a2a)
[![Go Report Card](https://goreportcard.com/badge/github.com/mashiike/a2a)](https://goreportcard.com/report/github.com/mashiike/a2a)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

Agent2Agent (A2A) is a Go library implementing an open protocol designed to facilitate communication between agents. This library provides features such as task management, session management, streaming, push notifications, and history tracking to streamline agent-to-agent collaboration.

For more details about the A2A protocol, refer to the [official documentation](https://google.github.io/A2A/#/documentation).

## Installation

Install the library using the following command:

```bash
go get github.com/mashiike/a2a
```

## Usage

### Creating a Server

The following is an example of creating an A2A server:

```go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/mashiike/a2a"
)

func main() {
	card := &a2a.AgentCard{
		Name:    "Example Agent",
		URL:     "http://localhost:8080",
		Version: "1.0.0",
		Skills: []a2a.Skill{
			{
				ID:   "example-skill",
				Name: "Example Skill",
				Tags: []string{"example"},
			},
		},
	}

	agent := a2a.AgentFunc(func(ctx context.Context, tr a2a.TaskResponder, task *a2a.Task) error {
		log.Printf("Received task: %s", task.ID)
		tr.SetStatus(ctx, a2a.TaskStatus{State: a2a.TaskStateWorking}, false, nil)
		tr.WriteArtifact(ctx, a2a.Artifact{
			Index: 0,
			Parts: []a2a.Part{
				a2a.TextPart("Hello, this is a response from the server."),
			},
		}, nil)
		tr.SetStatus(ctx, a2a.TaskStatus{State: a2a.TaskStateCompleted}, true, nil)
		return nil
	})

	handler, err := a2a.NewHandler(card, agent, nil)
	if err != nil {
		log.Fatalf("Failed to create handler: %v", err)
	}

	log.Println("Starting server on :8080")
	http.ListenAndServe(":8080", handler)
}
```

The server functionality can be implemented by registering a struct that satisfies the following `Agent` interface:

```go
type Agent interface {
	Invoke(ctx context.Context, r TaskResponder, task *Task) error
}
```

The above example synchronously returns results, but you can also process tasks asynchronously using Go routines. By replacing certain infrastructure implementations, the server can be made more practical. These replacements can be configured using the third argument of `NewHandler`.

For more details, refer to the [godoc](https://pkg.go.dev/github.com/mashiike/a2a#HandlerOptions).

#### Store Interface

The `Store` interface manages tasks and their associated data. If the server operates asynchronously or across multiple server instances, it is recommended to replace the default `InMemoryStore` with a custom implementation. 

The `store/s3` package provides a `Store` implementation that uses Amazon S3 for storage. This is useful for distributed systems or when persistent storage is required.

The `Store` interface is defined in the `store` package, and you can implement your own `Store` interface to use a different storage backend.

```go
type Store interface {
	UpsertTask(ctx context.Context, params TaskSendParams) (*Task, error)
	GetTask(ctx context.Context, taskID string, historyLength *int) (*Task, error)
	AppendHistory(ctx context.Context, taskID string, message Message) error
	UpdateStatus(ctx context.Context, taskID string, status TaskStatus) error
	UpdateArtifact(ctx context.Context, taskID string, artifact Artifact) error
}
```

Additionally, implementing the `PushNotificationStore` interface allows for managing push notifications for tasks.

```go
type PushNotificationStore interface {
	CreateTaskPushNotification(ctx context.Context, cfg *TaskPushNotificationConfig) error
	GetTaskPushNotification(ctx context.Context, taskID string) (*TaskPushNotificationConfig, error)
}
```

#### PubSub Interface

The `PubSub` interface is required when setting `capabilities.streaming` to `true`.

The default `ChannelPubSub` is a `PubSub` implementation that uses Go channels for communication. Additionally, the `pubsub/redis` package provides a `PubSub` implementation that uses Redis for communication, which is suitable for distributed systems.

```go
type PubSub interface {
	Publish(ctx context.Context, event StreamingEvent) error
	Subscribe(ctx context.Context, taskID string) (<-chan StreamingEvent, error)
}
```

When updating the task state, the `Handler` uses `Publish` to distribute change information if `HandlerOptions.TaskEventQueue` is not `nil`. Additionally, the `tasks/sendSubscribe` and `tasks/resubscribe` RPC methods use `Subscribe` to receive change information.

### Creating a Client

The following is an example of creating a client to communicate with an A2A server:

```go
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"

	"github.com/google/uuid"
	"github.com/mashiike/a2a"
)

func main() {
	client, err := a2a.NewClient("http://localhost:8080")
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	params := a2a.TaskSendParams{
		ID: uuid.New().String(),
		Message: a2a.Message{
			Role: a2a.MessageRoleUser,
			Parts: []a2a.Part{
				a2a.TextPart("Hello, Agent!"),
			},
		},
	}

	ctx := context.Background()
	task, err := client.SendTask(ctx, params)
	if err != nil {
		log.Fatalf("Failed to send task: %v", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(task)
}
```

## License

This project is licensed under the MIT License. See the [LICENSE](./LICENSE) file for details.
