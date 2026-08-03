package project

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Error is a stable, machine-readable failure from the project command.
type Error struct {
	Code               string   `json:"code"`
	Command            string   `json:"command"`
	Implementation     string   `json:"implementation,omitempty"`
	DetectedMarker     string   `json:"detected_marker,omitempty"`
	Capability         string   `json:"capability,omitempty"`
	RequestedArguments []string `json:"requested_arguments,omitempty"`
	Tool               string   `json:"tool,omitempty"`
	Source             string   `json:"source,omitempty"`
	Hint               string   `json:"hint,omitempty"`
	Stdout             string   `json:"stdout,omitempty"`
	Stderr             string   `json:"stderr,omitempty"`
	ExitStatus         int      `json:"exit_status"`
	Cause              error    `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	message := e.Code
	if e.Cause != nil {
		message += ": " + e.Cause.Error()
	}
	if e.Hint != "" {
		message += " (hint: " + e.Hint + ")"
	}
	return message
}

func (e *Error) Unwrap() error { return e.Cause }

// MarshalJSON keeps structured errors independent of the concrete cause.
func (e *Error) MarshalJSON() ([]byte, error) {
	type alias Error
	return json.Marshal((*alias)(e))
}

func projectError(code, message string) *Error {
	return &Error{Code: code, Command: "project", ExitStatus: 1, Cause: fmt.Errorf("%s", message)}
}

func wrapProjectError(code, message string, cause error) *Error {
	return &Error{Code: code, Command: "project", ExitStatus: 1, Cause: fmt.Errorf("%s: %w", message, cause)}
}

func withDetection(err error, detection Detection) *Error {
	if err == nil {
		return nil
	}
	var projectErr *Error
	if !errors.As(err, &projectErr) {
		projectErr = &Error{Code: "project_operation_failed", Command: "project", ExitStatus: 1, Cause: err}
	}
	projectErr.Implementation = detection.ProfileID
	projectErr.DetectedMarker = detection.Marker
	return projectErr
}
