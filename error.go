package a2a

import (
	"errors"
)

var (
	ErrStreaminNotSupported              = errors.New("streaming not supported")
	ErrAgentCardRequired                 = errors.New("agent card required")
	ErrTaskNotFound                      = errors.New("task not found")
	ErrTaskArtifactNotFound              = errors.New("task artifact not found")
	ErrTaskArtifactIndexInvalid          = errors.New("task artifact index invalid")
	ErrTaskAlreadyFinalized              = errors.New("task already finalized")
	ErrTaskPushNotificationNotConfigured = errors.New("task push notification not configured")
)
