package chat

import "errors"

var (
	ErrInvalidID              = errors.New("invalid chat id")
	ErrInvalidTmuxSession     = errors.New("invalid tmux session")
	ErrInvalidRewindTimestamp = errors.New("invalid rewind timestamp")
	ErrChatRunning            = errors.New("chat has an active run")
	ErrNotFound               = errors.New("chat not found")
	ErrInvalidAutopilot       = errors.New("autopilot limits are out of range")
	ErrInvalidTeam            = errors.New("team settings are out of range")
)
