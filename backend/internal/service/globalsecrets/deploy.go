package globalsecrets

// Deploy access: whether one project's agents may reach one live server.
//
// The switch is not a policy an agent is asked to respect. It is the scope on
// the vault entry that holds the key, so turning it off removes the project
// from that scope and the next sync deletes /root/.ssh/<name> and its config
// block out of the container. An agent with the switch off cannot deploy for
// the same reason it cannot deploy to a server it has never heard of: there is
// no key and no host entry.
//
// That is the whole design. Everything below is a readable way to say "add or
// remove this project from this target's scope, then converge the container".

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
)

// ErrNotSSHSecret is returned when a caller aims the deploy switch at an entry
// that holds no SSH target — an env or file secret grants no server access, so
// there is nothing to switch.
var ErrNotSSHSecret = errors.New("deploy access applies to ssh entries only")

// DeployTarget is one live server a project could be allowed to reach.
type DeployTarget struct {
	// Key is the vault entry, and the identifier every call below uses.
	Key string `json:"key"`
	// Name is what the agent types: `ssh <name>`.
	Name string `json:"name"`
	// Domain is the site, when the operator recorded one; Host is always
	// present. The UI shows the domain and falls back to the host.
	Domain string `json:"domain,omitempty"`
	Host   string `json:"host"`
	User   string `json:"user"`
	Port   int    `json:"port"`
	// Enabled reports whether this project is currently in scope, which is to
	// say whether the key is in its container right now.
	Enabled bool `json:"enabled"`
}

// Label is what to show a human: the domain if there is one, else the host.
func (t DeployTarget) Label() string {
	if t.Domain != "" {
		return t.Domain
	}
	return t.Host
}

// DeployTargets lists every SSH target in the vault with this project's access
// state, so a settings panel and a chat header can render the same answer.
func (s *Service) DeployTargets(ctx context.Context, projectID string) ([]DeployTarget, error) {
	if s == nil || s.store == nil {
		return nil, ErrUnavailable
	}
	secrets, err := s.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DeployTarget, 0)
	for _, secret := range secrets {
		if secret.Kind != KindSSH || secret.SSH == nil {
			continue
		}
		out = append(out, DeployTarget{
			Key:     secret.Key,
			Name:    secret.SSH.Name,
			Domain:  secret.SSH.Domain,
			Host:    secret.SSH.Host,
			User:    secret.SSH.User,
			Port:    secret.SSH.EffectivePort(),
			Enabled: secret.Scope.Includes(projectID),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label() < out[j].Label() })
	return out, nil
}

// SetDeployAccess turns one project's access to one server on or off.
//
// It returns the resulting state so a caller never has to guess, and it
// converges the container before returning: an operator who switched access
// off is entitled to assume the key is gone by the time the request answers,
// not at some later moment.
func (s *Service) SetDeployAccess(
	ctx context.Context,
	projectID, key string,
	enabled bool,
	actor string,
) (DeployTarget, error) {
	if s == nil || s.store == nil {
		return DeployTarget{}, ErrUnavailable
	}
	projectID = strings.TrimSpace(projectID)
	key = strings.TrimSpace(key)
	if projectID == "" || key == "" {
		return DeployTarget{}, ErrNotFound
	}

	secrets, err := s.load(ctx)
	if err != nil {
		return DeployTarget{}, err
	}
	index := -1
	for i, secret := range secrets {
		if secret.Key == key {
			index = i
			break
		}
	}
	if index < 0 {
		return DeployTarget{}, ErrNotFound
	}
	if secrets[index].Kind != KindSSH || secrets[index].SSH == nil {
		return DeployTarget{}, ErrNotSSHSecret
	}

	// "All projects" is a wider grant than this switch can express, so it is
	// left alone rather than silently narrowed: an operator who set it that
	// way did so somewhere else, and turning one project off here must not
	// quietly revoke every other project's access.
	if secrets[index].Scope.All {
		if enabled {
			return s.deployTarget(secrets[index], projectID), nil
		}
		return DeployTarget{}, ErrInvalidScope
	}

	secrets[index].Scope = toggleProject(secrets[index].Scope, projectID, enabled)
	secrets[index].UpdatedAt = s.now().UnixMilli()
	secrets[index].UpdatedBy = strings.ToLower(strings.TrimSpace(actor))
	if err := s.store.Save(ctx, secrets); err != nil {
		return DeployTarget{}, err
	}
	s.record(ctx, audit.ActionSettingsSecretUpdate, key, audit.Meta{
		"deploy":  enabled,
		"project": projectID,
		"target":  secrets[index].SSH.Name,
	}, nil)

	// Converge this project alone. A change here says nothing about any other
	// project's access, so nothing else needs touching.
	s.resync(ctx, Scope{ProjectIDs: []string{projectID}})
	return s.deployTarget(secrets[index], projectID), nil
}

func (s *Service) deployTarget(secret Secret, projectID string) DeployTarget {
	return DeployTarget{
		Key:     secret.Key,
		Name:    secret.SSH.Name,
		Domain:  secret.SSH.Domain,
		Host:    secret.SSH.Host,
		User:    secret.SSH.User,
		Port:    secret.SSH.EffectivePort(),
		Enabled: secret.Scope.Includes(projectID),
	}
}

// toggleProject adds or removes one project id, keeping the list sorted and
// free of duplicates so two identical scopes always compare equal on disk.
func toggleProject(scope Scope, projectID string, enabled bool) Scope {
	ids := make([]string, 0, len(scope.ProjectIDs)+1)
	for _, id := range scope.ProjectIDs {
		if id != projectID {
			ids = append(ids, id)
		}
	}
	if enabled {
		ids = append(ids, projectID)
	}
	sort.Strings(ids)
	return Scope{All: false, ProjectIDs: ids}
}
