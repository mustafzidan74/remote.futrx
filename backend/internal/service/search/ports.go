package search

import (
	"context"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// ChatSource is the read side of the chat repository the initial index build
// walks. It is the same repository the rest of the platform reads through, so
// the index can never see a transcript the chat views cannot.
type ChatSource interface {
	List(ctx context.Context) ([]servicechat.Meta, error)
	ReadEvents(ctx context.Context, id servicechat.ID) ([]servicechat.Event, error)
}

// ChatDirectory answers "which chats may this caller see?". It is satisfied by
// the chat access service, which is also what the sidebar listing uses.
type ChatDirectory interface {
	List(ctx context.Context, email string, isAdmin bool) ([]servicechat.Meta, error)
}

// ProjectDirectory supplies project names for the result rows.
type ProjectDirectory interface {
	ListVisible(ctx context.Context, callerEmail string, isAdmin bool) ([]serviceproject.Meta, error)
}
