package project

import "errors"

var (
	ErrNameRequired       = errors.New("name is required")
	ErrInvalidID          = errors.New("invalid project id")
	ErrNotFound           = errors.New("project not found")
	ErrInvalidSecretKey   = errors.New("invalid secret key (must match [A-Za-z_][A-Za-z0-9_]*)")
	ErrInvalidLimits      = errors.New("invalid container resource limits")
	ErrSecretsUnavailable = errors.New("secrets store is not configured")
	ErrUnknownTemplate    = errors.New("unknown project template")
	// ErrInvalidTemplateInput is the class every template-input rejection
	// belongs to. Rejections carry their own message (which input, and why),
	// so handlers match the class and forward the message.
	ErrInvalidTemplateInput = errors.New("invalid template input")
)
