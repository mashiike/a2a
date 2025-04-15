package a2a

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamingEventMarshalUnmarshal(t *testing.T) {
	// Test case for StatusUpdated
	statusEvent := &StreamingEvent{
		StatusUpdated: &TaskStatusUpdateEvent{
			ID: "status1",
			Status: TaskStatus{
				State: TaskStateWorking,
			},
			Final: true,
		},
	}

	// Marshal the StatusUpdated event
	data, err := json.Marshal(statusEvent)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"status1","status":{"state":"working"},"final":true}`, string(data))

	// Unmarshal the StatusUpdated event
	var unmarshaledStatusEvent StreamingEvent
	err = json.Unmarshal(data, &unmarshaledStatusEvent)
	require.NoError(t, err)
	require.Equal(t, statusEvent, &unmarshaledStatusEvent)

	// Test case for ArtifactUpdated
	artifactEvent := &StreamingEvent{
		ArtifactUpdated: &TaskArtifactUpdateEvent{
			ID: "artifact1",
			Artifact: Artifact{
				Parts: []Part{
					TextPart("example text"),
				},
				Index: 0,
			},
		},
	}

	// Marshal the ArtifactUpdated event
	data, err = json.Marshal(artifactEvent)
	require.NoError(t, err)
	require.JSONEq(t, `{"id":"artifact1","artifact":{"parts":[{"type":"text","text":"example text"}],"index":0}}`, string(data))

	// Unmarshal the ArtifactUpdated event
	var unmarshaledArtifactEvent StreamingEvent
	err = json.Unmarshal(data, &unmarshaledArtifactEvent)
	require.NoError(t, err)
	require.Equal(t, artifactEvent, &unmarshaledArtifactEvent)

	// Test case for invalid event
	invalidData := `{"unknown_field":"value"}`
	var invalidEvent StreamingEvent
	err = json.Unmarshal([]byte(invalidData), &invalidEvent)
	require.Error(t, err)
}
