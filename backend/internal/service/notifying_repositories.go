package service

import (
	"context"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/service/workspacehub"
)

type notifyingChatRepository struct {
	servicechat.Repository
	workspace *workspacehub.Hub
	running   func(servicechat.ID) bool
}

func (r notifyingChatRepository) Create(ctx context.Context, meta servicechat.Meta) (servicechat.Meta, error) {
	next, err := r.Repository.Create(ctx, meta)
	if err == nil {
		r.workspace.PublishChatUpsert(r.withRunning(next))
	}
	return next, err
}

func (r notifyingChatRepository) Update(
	ctx context.Context,
	id servicechat.ID,
	fn func(*servicechat.Meta),
) (servicechat.Meta, error) {
	next, err := r.Repository.Update(ctx, id, fn)
	if err == nil {
		r.workspace.PublishChatUpsert(r.withRunning(next))
	}
	return next, err
}

func (r notifyingChatRepository) Delete(ctx context.Context, id servicechat.ID) error {
	err := r.Repository.Delete(ctx, id)
	if err == nil {
		r.workspace.PublishChatDelete(id)
	}
	return err
}

func (r notifyingChatRepository) AppendEvent(
	ctx context.Context,
	id servicechat.ID,
	ev servicechat.Event,
) (servicechat.Event, error) {
	next, err := r.Repository.AppendEvent(ctx, id, ev)
	if err == nil && eventUpdatesWorkspace(next.Type) {
		r.publishChat(ctx, id)
	}
	return next, err
}

func (r notifyingChatRepository) TruncateEventsBefore(
	ctx context.Context,
	id servicechat.ID,
	beforeT int64,
) ([]servicechat.Event, error) {
	events, err := r.Repository.TruncateEventsBefore(ctx, id, beforeT)
	if err == nil {
		r.publishChat(ctx, id)
	}
	return events, err
}

func (r notifyingChatRepository) publishChat(ctx context.Context, id servicechat.ID) {
	if meta, err := r.Repository.Get(ctx, id); err == nil {
		r.workspace.PublishChatUpsert(r.withRunning(meta))
	}
}

func (r notifyingChatRepository) withRunning(meta servicechat.Meta) servicechat.Meta {
	if r.running != nil {
		meta.Running = r.running(meta.ID)
	}
	return meta
}

func eventUpdatesWorkspace(eventType string) bool {
	return eventType == "user" || eventType == "complete" || eventType == "error"
}

type notifyingProjectRepository struct {
	serviceproject.Repository
	workspace *workspacehub.Hub
}

func (r notifyingProjectRepository) Create(ctx context.Context, meta serviceproject.Meta) (serviceproject.Meta, error) {
	next, err := r.Repository.Create(ctx, meta)
	if err == nil {
		r.workspace.PublishProjectUpsert(next)
	}
	return next, err
}

// Update is also how a project enters and leaves the trash. A trashed project
// must vanish from every connected sidebar immediately, so it is published as
// a deletion rather than an upsert; restoring it publishes the upsert that
// brings it back.
func (r notifyingProjectRepository) Update(
	ctx context.Context,
	id serviceproject.ID,
	fn func(*serviceproject.Meta),
) (serviceproject.Meta, error) {
	next, err := r.Repository.Update(ctx, id, fn)
	if err == nil {
		r.publish(next)
	}
	return next, err
}

func (r notifyingProjectRepository) publish(meta serviceproject.Meta) {
	if meta.Trashed() {
		r.workspace.PublishProjectDelete(meta.ID)
		return
	}
	r.workspace.PublishProjectUpsert(meta)
}

func (r notifyingProjectRepository) SetStatus(
	ctx context.Context,
	id serviceproject.ID,
	status serviceproject.Status,
	errMsg string,
) (serviceproject.Meta, error) {
	next, err := r.Repository.SetStatus(ctx, id, status, errMsg)
	if err == nil {
		r.publish(next)
	}
	return next, err
}

func (r notifyingProjectRepository) Delete(ctx context.Context, id serviceproject.ID) error {
	err := r.Repository.Delete(ctx, id)
	if err == nil {
		r.workspace.PublishProjectDelete(id)
	}
	return err
}
