package stores

import (
	"fmt"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	serviceskills "github.com/futrx-com/remote.futrx.com/internal/service/skills"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
	serviceuser "github.com/futrx-com/remote.futrx.com/internal/service/user"
	serviceusersettings "github.com/futrx-com/remote.futrx.com/internal/service/usersettings"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileaudit"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileauth"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filechat"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filenotify"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileplaybooks"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectaccess"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectsecrets"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectshares"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileresources"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileschedule"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileskillsglobal"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filesnapshot"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileusage"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileusers"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileusersettings"
)

type AuthStore interface {
	serviceauth.Store
}

type Stores struct {
	Chats          servicechat.Repository
	Projects       serviceproject.Repository
	ProjectSecrets serviceproject.SecretsRepository
	ProjectAccess  serviceproject.AccessRepository
	ProjectShares  serviceshare.Repository
	Snapshots      servicesnapshot.Repository
	Schedules      serviceschedule.Repository
	Resources      serviceresources.Repository
	Auth           AuthStore
	Users          serviceuser.Repository
	UserSettings   serviceusersettings.Repository
	Notifications  servicenotify.Store
	Playbooks      serviceplaybooks.Repository
	GlobalSkills   serviceskills.GlobalRepository
	Usage          serviceusage.Repository
	Audit          serviceaudit.Store
}

func New(dataDir string) (Stores, error) {
	chats, err := filechat.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init chat store: %w", err)
	}

	projects, err := fileproject.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init project store: %w", err)
	}

	projectSecrets, err := fileprojectsecrets.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init project secrets store: %w", err)
	}

	projectAccess, err := fileprojectaccess.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init project access store: %w", err)
	}

	projectShares, err := fileprojectshares.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init project shares store: %w", err)
	}

	snapshots, err := filesnapshot.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init snapshot store: %w", err)
	}

	schedules, err := fileschedule.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init scheduled tasks store: %w", err)
	}

	resources, err := fileresources.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init resource settings store: %w", err)
	}

	users, err := fileusers.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init users store: %w", err)
	}

	userSettings, err := fileusersettings.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init user settings store: %w", err)
	}

	notifications, err := filenotify.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init notification settings store: %w", err)
	}

	playbooks, err := fileplaybooks.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init playbooks store: %w", err)
	}

	globalSkills, err := fileskillsglobal.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init global skills store: %w", err)
	}

	usage, err := fileusage.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init usage store: %w", err)
	}

	auditLog, err := fileaudit.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init audit store: %w", err)
	}

	return Stores{
		Chats:          chats,
		Projects:       projects,
		ProjectSecrets: projectSecrets,
		ProjectAccess:  projectAccess,
		ProjectShares:  projectShares,
		Snapshots:      snapshots,
		Schedules:      schedules,
		Resources:      resources,
		Auth:           fileauth.New(dataDir),
		Users:          users,
		UserSettings:   userSettings,
		Notifications:  notifications,
		Playbooks:      playbooks,
		GlobalSkills:   globalSkills,
		Usage:          usage,
		Audit:          auditLog,
	}, nil
}
