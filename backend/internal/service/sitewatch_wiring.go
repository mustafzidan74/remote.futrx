package service

import (
	"context"
	"strings"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
)

// The client-site watcher deliberately knows nothing about projects: its
// Access and Catalog ports speak in plain strings. The two adapters below are
// the only place the two subsystems meet, which is the same shape the
// dashboard and notification bridges use.

// siteWatchAccess answers the watcher's visibility question — "may this
// person see this project?" — from the project membership list.
type siteWatchAccess struct {
	projects *serviceproject.Service
}

var _ servicesitewatch.Access = siteWatchAccess{}

func (a siteWatchAccess) HasProjectAccess(ctx context.Context, projectID, email string) (bool, error) {
	if a.projects == nil || strings.TrimSpace(projectID) == "" {
		return false, nil
	}
	return a.projects.HasAccess(ctx, serviceproject.ID(projectID), email)
}

// siteWatchCatalog suggests sites from the domains projects already store in
// their own secrets, which is how a Hestia-provisioned workspace records the
// hostname it serves.
//
// It reads the secrets repository directly rather than through the project
// service, for the same reason the env-sync paths do: this is a background
// scan across every project, and recording it as a human secret read would
// bury the audit log under noise nobody asked for.
type siteWatchCatalog struct {
	projects serviceproject.Repository
	secrets  serviceproject.SecretsRepository
}

var _ servicesitewatch.Catalog = siteWatchCatalog{}

// domainSecretKeys are the secret names that hold "the public hostname this
// project serves". HESTIA_DOMAIN is the one a Hestia-provisioned box writes;
// the rest are the conventional names the same information arrives under.
var domainSecretKeys = map[string]struct{}{
	"HESTIA_DOMAIN":  {},
	"SITE_DOMAIN":    {},
	"SITE_URL":       {},
	"PUBLIC_URL":     {},
	"APP_URL":        {},
	"WP_HOME":        {},
	"WP_SITEURL":     {},
	"PRIMARY_DOMAIN": {},
}

func (c siteWatchCatalog) Candidates(ctx context.Context) ([]servicesitewatch.Candidate, error) {
	if c.projects == nil || c.secrets == nil {
		return nil, nil
	}
	metas, err := c.projects.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]servicesitewatch.Candidate, 0, len(metas))
	for _, meta := range metas {
		secrets, err := c.secrets.List(ctx, meta.ID)
		if err != nil {
			// One unreadable project must not lose the whole suggestion
			// list: the import is an offer, not a transaction.
			continue
		}
		seen := map[string]struct{}{}
		for _, secret := range secrets {
			if _, wanted := domainSecretKeys[strings.ToUpper(strings.TrimSpace(secret.Key))]; !wanted {
				continue
			}
			value := strings.TrimSpace(secret.Value)
			if value == "" {
				continue
			}
			if _, dupe := seen[strings.ToLower(value)]; dupe {
				continue
			}
			seen[strings.ToLower(value)] = struct{}{}
			out = append(out, servicesitewatch.Candidate{
				ProjectID:   string(meta.ID),
				ProjectName: meta.Name,
				Domain:      value,
				SecretKey:   secret.Key,
			})
		}
	}
	return out, nil
}
