package service

import (
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicedashboard "github.com/futrx-com/remote.futrx.com/internal/service/dashboard"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

// newDashboardService assembles the home-screen aggregator out of the
// services that already exist.
//
// It lives in its own file, and does its own nil-narrowing, for one reason:
// the dashboard reads nine subsystems, and any of them can legitimately be
// absent on a given deployment. Handing a nil *T straight to an interface
// field would produce a non-nil interface holding a nil pointer — an
// interface that passes `!= nil` and panics on first use — so every optional
// dependency is converted through a helper that keeps nil nil.
func newDashboardService(
	projects *serviceproject.Service,
	chats *servicechat.AccessService,
	usage *serviceusage.Service,
	health *servicehealth.Service,
	schedules *serviceschedule.Service,
	snapshots *servicesnapshot.Service,
	notifications *servicenotify.Service,
	platform *servicemonitoring.Service,
	capacity *serviceresources.Service,
	backups servicedashboard.Backups,
	trashRetention time.Duration,
) *servicedashboard.Service {
	if projects == nil {
		return nil
	}
	return servicedashboard.New(servicedashboard.Dependencies{
		Projects:       projects,
		Chats:          dashboardChats(chats),
		Usage:          dashboardUsage(usage),
		Health:         dashboardHealth(health),
		Schedules:      dashboardSchedules(schedules),
		Snapshots:      dashboardSnapshots(snapshots),
		Notifications:  dashboardNotifications(notifications),
		Platform:       dashboardPlatform(platform),
		Capacity:       dashboardCapacity(capacity),
		Backups:        backups,
		TrashRetention: trashRetention,
	})
}

func dashboardChats(service *servicechat.AccessService) servicedashboard.Chats {
	if service == nil {
		return nil
	}
	return service
}

func dashboardUsage(service *serviceusage.Service) servicedashboard.UsageLedger {
	if service == nil {
		return nil
	}
	return service
}

func dashboardHealth(service *servicehealth.Service) servicedashboard.Health {
	if service == nil {
		return nil
	}
	return service
}

func dashboardSchedules(service *serviceschedule.Service) servicedashboard.Schedules {
	if service == nil {
		return nil
	}
	return service
}

func dashboardSnapshots(service *servicesnapshot.Service) servicedashboard.Snapshots {
	if service == nil {
		return nil
	}
	return service
}

func dashboardNotifications(service *servicenotify.Service) servicedashboard.Notifications {
	if service == nil {
		return nil
	}
	return service
}

func dashboardPlatform(service *servicemonitoring.Service) servicedashboard.PlatformHealth {
	if service == nil {
		return nil
	}
	return service
}

func dashboardCapacity(service *serviceresources.Service) servicedashboard.Capacity {
	if service == nil {
		return nil
	}
	return service
}
