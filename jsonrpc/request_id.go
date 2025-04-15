package jsonrpc

import (
	"encoding/json"
	"fmt"
)

// RequestID represents a value that can be null, string, or integer.
type RequestID struct {
	StringValue string
	IntValue    int
	IsInt       bool
}

// UnmarshalJSON implements the json.Unmarshaler interface for RequestID.
func (n *RequestID) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as integer
	var intValue int
	if err := json.Unmarshal(data, &intValue); err == nil {
		n.IsInt = true
		n.IntValue = intValue
		n.StringValue = fmt.Sprintf("%d", intValue)
		return nil
	}

	// Try to unmarshal as string
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		n.IsInt = false
		n.StringValue = stringValue
		return nil
	}

	return fmt.Errorf("invalid value for RequestID: %s", string(data))
}

// String returns the string representation of the RequestID.
func (n RequestID) String() string {
	return n.StringValue
}

// MarshalJSON implements the json.Marshaler interface for RequestID.
func (n RequestID) MarshalJSON() ([]byte, error) {
	if n.IsInt {
		return json.Marshal(n.IntValue)
	}
	return json.Marshal(n.StringValue)
}
