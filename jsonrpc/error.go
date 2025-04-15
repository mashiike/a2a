package jsonrpc

import (
	"fmt"
	"net/http"
)

// ErrorMessage represents a JSON-RPC error message.
type ErrorMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface for ErrorMessage.
func (e *ErrorMessage) Error() string {
	return fmt.Sprintf("Error %d: %s", e.Code, e.Message)
}

// JSON-RPC standard error codes.
const (
	CodeParseError               = -32700 // Invalid JSON was sent
	CodeInvalidRequest           = -32600 // Request payload validation error
	CodeMethodNotFound           = -32601 // Not a valid method
	CodeInvalidParams            = -32602 // Invalid method parameters
	CodeInternalError            = -32603 // Internal JSON-RPC error
	CodeServerErrorStart         = -32000 // Start of server-specific errors
	CodeServerErrorEnd           = -32099 // End of server-specific errors
	CodeTaskNotFound             = -32001 // Task not found with the provided id
	CodeTaskCannotBeCanceled     = -32002 // Task cannot be canceled by the remote agent
	CodePushNotSupported         = -32003 // Push Notification is not supported by the agent
	CodeUnsupportedOperation     = -32004 // Operation is not supported
	CodeIncompatibleContentTypes = -32005 // Incompatible content types between client and agent
)

// CodeMessage returns the message corresponding to the given error code.
func CodeMessage(code int) string {
	switch code {
	case CodeParseError:
		return "JSON parse error"
	case CodeInvalidRequest:
		return "Invalid Request"
	case CodeMethodNotFound:
		return "Method not found"
	case CodeInvalidParams:
		return "Invalid params"
	case CodeInternalError:
		return "Internal error"
	case CodeTaskNotFound:
		return "Task not found"
	case CodeTaskCannotBeCanceled:
		return "Task cannot be canceled"
	case CodePushNotSupported:
		return "Push notifications not supported"
	case CodeUnsupportedOperation:
		return "Unsupported operation"
	case CodeIncompatibleContentTypes:
		return "Incompatible content types"
	default:
		if code >= CodeServerErrorStart && code <= CodeServerErrorEnd {
			return "Server error"
		}
		return "Unknown error"
	}
}

// CodeDescription returns a detailed description corresponding to the given error code.
func CodeDescription(code int) string {
	switch code {
	case CodeParseError:
		return "Invalid JSON was sent"
	case CodeInvalidRequest:
		return "Request payload validation error"
	case CodeMethodNotFound:
		return "Not a valid method"
	case CodeInvalidParams:
		return "Invalid method parameters"
	case CodeInternalError:
		return "Internal JSON-RPC error"
	case CodeTaskNotFound:
		return "Task not found with the provided id"
	case CodeTaskCannotBeCanceled:
		return "Task cannot be canceled by the remote agent"
	case CodePushNotSupported:
		return "Push Notification is not supported by the agent"
	case CodeUnsupportedOperation:
		return "Operation is not supported"
	case CodeIncompatibleContentTypes:
		return "Incompatible content types between client and an agent"
	default:
		if code >= CodeServerErrorStart && code <= CodeServerErrorEnd {
			return "Reserved for implementation specific error codes"
		}
		return "No description available for this error code"
	}
}

type Error struct {
	HTTPStatusCode int        `json:"status_code,omitempty"`
	ID             *RequestID `json:"id,omitempty"`
	Message        error      `json:"message,omitempty"`
}

func (e *Error) Error() string {
	var errStr string
	if e.ID != nil {
		errStr = fmt.Sprintf("Request ID %s: ", e.ID.String())
	}
	if e.HTTPStatusCode != 0 {
		errStr += fmt.Sprintf("HTTP Status %d %s: ", e.HTTPStatusCode, http.StatusText(e.HTTPStatusCode))
	}
	if e.Message != nil {
		errStr += e.Message.Error()
	}
	if errStr == "" {
		return "Unknown error"
	}
	return errStr
}

func (e *Error) Unwrap() error {
	if e.Message != nil {
		return e.Message
	}
	return nil
}

func (e *Error) Write(w http.ResponseWriter) {
	if e.HTTPStatusCode == 0 {
		e.HTTPStatusCode = http.StatusInternalServerError
	}
	response := Response{
		JSONRPC: "2.0",
		ID:      e.ID,
		Error: &ErrorMessage{
			Code:    e.HTTPStatusCode,
			Message: e.Message.Error(),
		},
	}
	response.Write(w, e.HTTPStatusCode)
}
