package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/mashiike/a2a"
)

func main() {
	// Add a flag for the server port
	var port string
	flag.StringVar(&port, "port", "8080", "server port")
	flag.Parse()

	// Configure the agent card
	card := &a2a.AgentCard{
		Name:    "Example Agent",
		URL:     "http://localhost:" + port,
		Version: "1.0.0",
		Skills: []a2a.Skill{
			{
				ID:   "example-skill",
				Name: "Example Skill",
				Tags: []string{"example"},
			},
		},
	}

	// Implement the agent
	agent := a2a.AgentFunc(func(ctx context.Context, tr a2a.TaskManager, task *a2a.Task) error {
		// Process the task
		log.Printf("Received task: %s", task.ID)
		m.SetStatus(ctx, a2a.TaskStatus{State: a2a.TaskStateWorking}, false, nil)
		m.WriteArtifact(ctx, a2a.Artifact{
			Index: 0,
			Parts: []a2a.Part{
				a2a.TextPart("Hello, this is a response from the server."),
			},
		}, nil)
		m.SetStatus(ctx, a2a.TaskStatus{State: a2a.TaskStateCompleted}, true, nil)
		return nil
	})

	// Create the handler
	handler, err := a2a.NewHandler(card, agent, nil)
	if err != nil {
		log.Fatalf("Failed to create handler: %v", err)
	}

	// Start the server
	log.Printf("Starting server on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
