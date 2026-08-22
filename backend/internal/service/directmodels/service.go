// Package directmodels answers a chat turn from a completion API rather than
// from an agent CLI in a container.
//
// It joins two things that already exist — the free-tier provider pool and the
// local auxiliary model — behind one list, because from the composer's side
// they are the same kind of thing: a model that can talk and cannot touch the
// repository. Which of the two a chat picked matters only when something stops
// working, and the source is stored so the error can say which.
//
// The pool's own failover is deliberately switched off here. A background job
// does not care which provider wrote a chat title, so it lets the pool choose;
// a chat is pinned to the model the operator picked, because the header says
// which model is answering and a silent failover would make that a lie.
package directmodels

import (
	"context"
	"errors"
	"strings"

	serviceauxmodel "github.com/futrx-com/remote.futrx.com/internal/service/auxmodel"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceprompt "github.com/futrx-com/remote.futrx.com/internal/service/prompt"
	serviceproviderpool "github.com/futrx-com/remote.futrx.com/internal/service/providerpool"
)

// ChatMaxTokens is the answer budget for one direct turn.
//
// Far larger than any background job allows, because this is a conversation
// and the reader wants a real answer. It also has to cover a reasoning model's
// thinking, which comes out of the same allowance: too small a number and a
// thinking model returns nothing at all.
const ChatMaxTokens = 4000

// Pool is as much of the free-tier provider pool as this package needs.
type Pool interface {
	Complete(ctx context.Context, request serviceproviderpool.Request) (serviceproviderpool.Result, error)
	Registry() serviceproviderpool.Registry
}

// Local is the local auxiliary model.
type Local interface {
	CompleteLocal(ctx context.Context, systemPrompt, userText string, maxTokens int) (string, error)
	Config() serviceauxmodel.Config
}

// Service resolves a chat's direct model and answers with it.
type Service struct {
	pool  Pool
	local Local
}

// New builds the responder. Either side may be nil — a deployment with no
// pool, or none configured locally, simply offers fewer choices.
func New(pool Pool, local Local) *Service {
	return &Service{pool: pool, local: local}
}

// Choices lists what the composer may offer right now.
//
// Read live rather than cached: an operator can enable a provider or change
// the local model at any moment, and a stale list either offers something that
// no longer works or hides something that does.
func (s *Service) Choices(ctx context.Context) []serviceprompt.DirectChoice {
	if s == nil {
		return nil
	}
	var choices []serviceprompt.DirectChoice

	if s.pool != nil {
		for _, provider := range s.pool.Registry().Providers {
			if !provider.Enabled || !provider.HasKey() {
				continue
			}
			for _, model := range provider.Models {
				choices = append(choices, serviceprompt.DirectChoice{
					Source:        servicechat.DirectSourcePool,
					ProviderID:    provider.ID,
					ProviderLabel: provider.Label,
					Model:         model.ID,
					ModelLabel:    modelLabel(model),
				})
			}
		}
	}

	if s.local != nil {
		config := s.local.Config()
		if config.Enabled && config.Configured() && strings.TrimSpace(config.Model) != "" {
			choices = append(choices, serviceprompt.DirectChoice{
				Source:        servicechat.DirectSourceLocal,
				ProviderLabel: "On this server",
				Model:         config.Model,
				ModelLabel:    config.Model,
			})
		}
	}

	return choices
}

// Answer serves one turn.
func (s *Service) Answer(
	ctx context.Context,
	req serviceprompt.DirectRequest,
) (serviceprompt.DirectResult, error) {
	if s == nil {
		return serviceprompt.DirectResult{}, serviceprompt.ErrNoDirectResponder
	}

	switch req.Model.Source {
	case servicechat.DirectSourceLocal:
		return s.answerLocally(ctx, req)
	case servicechat.DirectSourcePool:
		return s.answerFromPool(ctx, req)
	default:
		return serviceprompt.DirectResult{}, errors.New("this chat names no direct model")
	}
}

func (s *Service) answerFromPool(
	ctx context.Context,
	req serviceprompt.DirectRequest,
) (serviceprompt.DirectResult, error) {
	if s.pool == nil {
		return serviceprompt.DirectResult{}, errors.New("no AI providers are configured")
	}
	result, err := s.pool.Complete(ctx, serviceproviderpool.Request{
		Need: serviceproviderpool.Need{
			Job:         "chat",
			ProviderID:  req.Model.ProviderID,
			PreferModel: req.Model.Model,
		},
		System:    req.SystemPrompt,
		Prompt:    req.UserText,
		MaxTokens: ChatMaxTokens,
	})
	if err != nil {
		return serviceprompt.DirectResult{}, err
	}
	return serviceprompt.DirectResult{
		Text:             result.Text,
		ProviderID:       result.ProviderID,
		Model:            result.Model,
		PromptTokens:     result.PromptTokens,
		CompletionTokens: result.CompletionTokens,
	}, nil
}

func (s *Service) answerLocally(
	ctx context.Context,
	req serviceprompt.DirectRequest,
) (serviceprompt.DirectResult, error) {
	if s.local == nil {
		return serviceprompt.DirectResult{}, errors.New("no local model is configured")
	}
	text, err := s.local.CompleteLocal(ctx, req.SystemPrompt, req.UserText, ChatMaxTokens)
	if err != nil {
		return serviceprompt.DirectResult{}, err
	}
	return serviceprompt.DirectResult{Text: text, Model: s.local.Config().Model}, nil
}

// modelLabel prefers the operator's own name for a model over its id.
func modelLabel(model serviceproviderpool.Model) string {
	if label := strings.TrimSpace(model.Label); label != "" {
		return label
	}
	return model.ID
}
