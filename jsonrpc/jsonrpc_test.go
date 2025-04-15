package jsonrpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestMarshalUnmarshal(t *testing.T) {
	req := &Request{
		JSONRPC: "2.0",
		ID:      nil,
		Method:  "testMethod",
		Params:  json.RawMessage(`{"key":"value"}`),
	}

	// Marshal the request
	data, err := json.Marshal(req)
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","method":"testMethod","params":{"key":"value"}}`, string(data))

	// Unmarshal the request
	var unmarshaledReq Request
	err = json.Unmarshal(data, &unmarshaledReq)
	require.NoError(t, err)
	require.Equal(t, req, &unmarshaledReq)
}

func TestResponseMarshalUnmarshal(t *testing.T) {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      nil,
		Result:  json.RawMessage(`{"key":"value"}`),
		Error:   nil,
	}

	// Marshal the response
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","result":{"key":"value"}}`, string(data))

	// Unmarshal the response
	var unmarshaledResp Response
	err = json.Unmarshal(data, &unmarshaledResp)
	require.NoError(t, err)
	require.Equal(t, resp, &unmarshaledResp)
}
