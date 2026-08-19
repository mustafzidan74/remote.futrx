package stores

import (
	"fmt"

	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	serviceskills "github.com/futrx-com/remote.futrx.com/internal/service/skills"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	servicesnippets "github.com/futrx-com/remote.futrx.com/internal/service/snippets"
	servicetranscribe "github.com/futrx-com/remote.futrx.com/internal/service/transcribe"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
	serviceuser "github.com/futrx-com/remote.futrx.com/internal/service/user"
	serviceusersettings "github.com/futrx-com/remote.futrx.com/internal/service/usersettings"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileagentprefs"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileaudit"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileauth"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filechat"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filegithub"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileglobalsecrets"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filemcp"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filemonitoring"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filenotify"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileplaybooks"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileportal"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectaccess"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectsecrets"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileprojectshares"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileresources"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileschedule"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filescreenshot"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileskillsglobal"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filesnapshot"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filesnippets"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filetranscribe"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileusage"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileusers"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileusersettings"
)

type AuthStore interface {
	serviceauth.Store
}

// ScreenshotStore is the pair of ports the preview-screenshot service needs:
// the per-project record index and the PNG blobs beside it.
type ScreenshotStore interface {
	servicescreenshot.Repository
	servicescreenshot.Blobs
}

type Stores struct {
	Chats          servicechat.Repository
	Projects       serviceproject.Repository
	ProjectSecrets serviceproject.SecretsRepository
	ProjectAccess  serviceproject.AccessRepository
	ProjectShares  serviceshare.Repository
	Snapshots      servicesnapshot.Repository
	// Screenshots is both the per-project capture index and the PNG blob
	// store; one file-backed type satisfies both ports.
	Screenshots    ScreenshotStore
	ProjectPortals serviceportal.Repository
	Schedules      serviceschedule.Repository
	Resources      serviceresources.Repository
	Auth           AuthStore
	Users          serviceuser.Repository
	UserSettings   serviceusersettings.Repository
	Notifications  servicenotify.Store
	Monitoring     servicemonitoring.Store
	Playbooks      serviceplaybooks.Repository
	Snippets       servicesnippets.Repository
	GlobalSkills   serviceskills.GlobalRepository
	GlobalSecrets  serviceglobalsecrets.Store
	// MCPServers is the platform MCP registry; ProjectMCP is the per-project
	// override document beside it. Either one nil leaves the registry
	// unavailable rather than half-wired.
	MCPServers    servicemcp.Store
	ProjectMCP    servicemcp.ProjectStore
	Usage         serviceusage.Repository
	Transcription servicetranscribe.Store
	Audit         serviceaudit.Store
	// ScheduleHistory is the per-task run log; it lives beside the task
	// catalog but is written append-only in its own files.
	ScheduleHistory serviceschedule.HistoryRepository
	// GitHub backs the per-project repository automation settings: the
	// webhook secret, the automation toggles, and the delivery log.
	GitHub servicegithub.Store
	// AgentPreferences backs the platform-wide agent reply preferences.
	AgentPreferences serviceagentprefs.Repository
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
	screenshots, err := filescreenshot.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init screenshot store: %w", err)
	}
	projectPortals, err := fileportal.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init client portal store: %w", err)
	}
	schedules, err := fileschedule.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init scheduled tasks store: %w", err)
	}
	scheduleHistory, err := fileschedule.NewHistory(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init scheduled task history store: %w", err)
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
	monitoring, err := filemonitoring.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init monitoring settings store: %w", err)
	}
	playbooks, err := fileplaybooks.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init playbooks store: %w", err)
	}
	snippets, err := filesnippets.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init snippets store: %w", err)
	}
	globalSkills, err := fileskillsglobal.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init global skills store: %w", err)
	}
	globalSecrets, err := fileglobalsecrets.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init secrets vault store: %w", err)
	}
	mcpServers, err := filemcp.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init MCP registry store: %w", err)
	}
	projectMCP, err := filemcp.NewProjectStore(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init project MCP store: %w", err)
	}
	gitHub, err := filegithub.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init github settings store: %w", err)
	}
	usage, err := fileusage.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init usage store: %w", err)
	}
	transcription, err := filetranscribe.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init transcription settings store: %w", err)
	}
	auditLog, err := fileaudit.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init audit store: %w", err)
	}
	agentPreferences, err := fileagentprefs.New(dataDir)
	if err != nil {
		return Stores{}, fmt.Errorf("init agent preferences store: %w", err)
	}
	return Stores{
		Chats:            chats,
		Projects:         projects,
		ProjectSecrets:   projectSecrets,
		ProjectAccess:    projectAccess,
		ProjectShares:    projectShares,
		Snapshots:        snapshots,
		Screenshots:      screenshots,
		ProjectPortals:   projectPortals,
		Schedules:        schedules,
		Resources:        resources,
		Auth:             fileauth.New(dataDir),
		Users:            users,
		UserSettings:     userSettings,
		Notifications:    notifications,
		Monitoring:       monitoring,
		Playbooks:        playbooks,
		Snippets:         snippets,
		GlobalSkills:     globalSkills,
		GlobalSecrets:    globalSecrets,
		MCPServers:       mcpServers,
		ProjectMCP:       projectMCP,
		Usage:            usage,
		Transcription:    transcription,
		Audit:            auditLog,
		ScheduleHistory:  scheduleHistory,
		GitHub:           gitHub,
		AgentPreferences: agentPreferences,
	}, nil
}
