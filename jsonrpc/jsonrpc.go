package jsonrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Request represents a unified structure for all request types.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID      `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// NewRequest creates a new Request with the specified method and parameters.
func NewRequest(method string, params any) (*Request, error) {
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal params: %w", err)
	}
	return &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  rawParams,
	}, nil
}

// Response represents a unified structure for all response types.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *RequestID      `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorMessage   `json:"error,omitempty"`
}

func (r *Response) Write(w http.ResponseWriter, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(r)
}

// WriteError writes a JSON-RPC error response to the provided http.ResponseWriter.
// It constructs an error response based on the given Request and error, and sets the HTTP status code.
// If the provided error is not of type ErrorMessage, a generic internal error message is created.
//
// Parameters:
//   - w: The http.ResponseWriter to write the response to.
//   - rpcReq: The original JSON-RPC request. Can be nil.
//   - err: The error to include in the response.
//   - code: The HTTP status code to set in the response.
func WriteError(w http.ResponseWriter, rpcReq *Request, err error, code int) {
	var id *RequestID
	if rpcReq != nil {
		id = rpcReq.ID
	}
	rpcErr := &Error{
		HTTPStatusCode: code,
		ID:             id,
	}
	var errMeg *ErrorMessage
	if errors.As(err, &errMeg) {
		rpcErr.Message = errMeg
	} else {
		// If the error is not a ErrorMessage, create a generic one
		rpcErr.Message = &ErrorMessage{
			Code:    CodeInternalError,
			Message: CodeMessage(CodeInternalError),
			Data:    map[string]any{"detail": err.Error()},
		}
	}
	rpcErr.Write(w)
}

// WriteResult writes a JSON-RPC success response to the provided http.ResponseWriter.
// It constructs a response with the given result and sets the HTTP status code.
// If the result cannot be marshaled to JSON, an internal error response is written instead.
//
// Parameters:
//   - w: The http.ResponseWriter to write the response to.
//   - rpcReq: The original JSON-RPC request.
//   - result: The result to include in the response. Can be nil.
//   - code: The HTTP status code to set in the response.
func WriteResult(w http.ResponseWriter, rpcReq *Request, result any, code int) {
	rpcResp := &Response{
		JSONRPC: "2.0",
		ID:      rpcReq.ID,
		Result:  nil,
		Error:   nil,
	}
	if result != nil {
		rawResult, err := json.Marshal(result)
		if err != nil {
			WriteError(w, rpcReq, fmt.Errorf("failed to marshal result: %w", err), http.StatusInternalServerError)
			return
		}
		rpcResp.Result = rawResult
	}
	rpcResp.Write(w, code)
}
