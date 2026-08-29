package audit

// Action names are dot-separated and hierarchical so the read API can filter
// by prefix: "project." matches every project action, "project.secret."
// matches only the secret ones. Keep the segments stable — operators build
// queries and alerts against these strings.
const (
	ActionAuthLoginSuccess = "auth.login.success"
	ActionAuthLoginFailure = "auth.login.failure"
	ActionAuthLogout       = "auth.logout"
	ActionAuthAdminClaim   = "auth.admin.claim"

	ActionUserInvite     = "user.invite"
	ActionUserRemove     = "user.remove"
	ActionUserRoleChange = "user.role-change"

	ActionProjectCreate       = "project.create"
	ActionProjectRename       = "project.rename"
	ActionProjectDelete       = "project.delete"
	ActionProjectTrash        = "project.trash"
	ActionProjectRestore      = "project.restore"
	ActionProjectPurge        = "project.purge"
	ActionProjectMemberAdd    = "project.member.add"
	ActionProjectMemberRemove = "project.member.remove"

	ActionProjectSecretRead   = "project.secret.read"
	ActionProjectSecretSet    = "project.secret.set"
	ActionProjectSecretDelete = "project.secret.delete"

	ActionProjectContainerStart   = "project.container.start"
	ActionProjectContainerStop    = "project.container.stop"
	ActionProjectContainerRestart = "project.container.restart"
	ActionProjectContainerRecycle = "project.container.recycle"
	ActionProjectContainerRepair  = "project.container.repair-network"
	ActionProjectContainerLimits  = "project.container.limits"

	ActionProjectBrowserStart    = "project.browser.start"
	ActionProjectBrowserStop     = "project.browser.stop"
	ActionProjectBrowserNavigate = "project.browser.navigate"
	// ActionProjectScreenshot records a headless capture of one preview port,
	// including whether it was pushed out through a notification sink.
	ActionProjectScreenshot = "project.screenshot"
	// ActionProjectLighthouse records a local Lighthouse audit, and the one-off
	// install of the CLI into a container that predates it. The install is
	// recorded because it changes what is inside a project's container, which
	// is exactly the kind of thing an operator later wants to find.
	ActionProjectLighthouse = "project.lighthouse"
	ActionSnapshotCreate    = "snapshot.create"
	ActionSnapshotRestore   = "snapshot.restore"
	ActionSnapshotDelete    = "snapshot.delete"

	// GitHub integration. The link and the automation toggles are project
	// actions; a webhook delivery is recorded under github.webhook so an
	// operator can filter inbound repository traffic on its own.
	ActionProjectGitHubLink     = "project.github.link"
	ActionProjectGitHubUnlink   = "project.github.unlink"
	ActionProjectGitHubClone    = "project.github.clone"
	ActionProjectGitHubPR       = "project.github.pr-create"
	ActionProjectGitHubImport   = "project.github.import-comments"
	ActionProjectGitHubSettings = "project.github.settings"

	ActionGitHubWebhookReceived = "github.webhook.received"
	ActionGitHubWebhookRejected = "github.webhook.rejected"

	ActionPortalEnable  = "portal.enable"
	ActionPortalRotate  = "portal.rotate"
	ActionPortalDisable = "portal.disable"

	ActionChatCreate = "chat.create"
	ActionChatDelete = "chat.delete"
	// ActionChatTranscribe records a voice dictation sent to the server
	// transcription provider. Its meta carries the clip duration only.
	ActionChatTranscribe = "chat.transcribe"

	ActionAgentRunStart  = "agent.run.start"
	ActionAgentRunCancel = "agent.run.cancel"

	ActionScheduleCreate = "schedule.create"
	ActionScheduleUpdate = "schedule.update"
	ActionScheduleArm    = "schedule.arm"
	ActionScheduleDelete = "schedule.delete"
	ActionScheduleRunNow = "schedule.run-now"

	ActionSettingsGoogleOAuth     = "settings.google-oauth.configure"
	ActionSettingsPlaybooks       = "settings.playbooks.update"
	ActionSettingsAgentConnect    = "settings.agent.connect"
	ActionSettingsAgentDisconnect = "settings.agent.disconnect"
	ActionSettingsTranscription   = "settings.transcription.configure"
	// ActionSettingsAgentPreferences records an edit to the platform-wide
	// agent reply preferences (language, tone, house rules).
	ActionSettingsAgentPreferences = "settings.agent-preferences.update"

	ActionSettingsSecretCreate = "settings.secret.create"
	ActionSettingsSecretUpdate = "settings.secret.update"
	ActionSettingsSecretDelete = "settings.secret.delete"
	ActionSettingsSecretTest   = "settings.secret.test"

	// ActionSettingsModelRouting records an edit to the automatic model
	// routing policy: the master switch, the rule list, and the two poles the
	// savings report prices against.
	ActionSettingsModelRouting = "settings.model-routing.update"

	// MCP registry edits. The project action records a member switching
	// servers on or off for one project, so its target is the project id.
	ActionSettingsMCPCreate  = "settings.mcp.create"
	ActionSettingsMCPUpdate  = "settings.mcp.update"
	ActionSettingsMCPDelete  = "settings.mcp.delete"
	ActionSettingsMCPTest    = "settings.mcp.test"
	ActionSettingsMCPProject = "settings.mcp.project-update"

	// Free-tier provider pool edits. The pool action covers the global policy
	// — auto-switch, the preferred provider, and the priority order — because
	// those are one setting from an operator's point of view.
	ActionSettingsProviderCreate = "settings.provider.create"
	ActionSettingsProviderUpdate = "settings.provider.update"
	ActionSettingsProviderDelete = "settings.provider.delete"
	ActionSettingsProviderTest   = "settings.provider.test"
	ActionSettingsProviderPool   = "settings.provider.pool-update"

	// ActionProviderComplete records one bulk-lane completion. Its meta
	// carries the provider, the model and the token counts — never the
	// prompt and never the answer.
	ActionProviderComplete = "provider.complete"

	// Third-party agent endpoint edits. The register names vendors, base
	// URLs, and vault key *references*; no action here ever records a key.
	ActionSettingsAgentEndpointCreate = "settings.agent-endpoint.create"
	ActionSettingsAgentEndpointUpdate = "settings.agent-endpoint.update"
	ActionSettingsAgentEndpointDelete = "settings.agent-endpoint.delete"
	ActionSettingsAgentEndpointTest   = "settings.agent-endpoint.test"

	ActionSelfUpdateTrigger = "self-update.trigger"

	ActionWorkspaceFileUpload      = "workspace.file.upload"
	ActionWorkspaceFileDownload    = "workspace.file.download"
	ActionWorkspaceArchiveDownload = "workspace.file.archive-download"
	ActionWorkspaceGitCheckout     = "workspace.git.checkout"
	ActionWorkspaceIDEOpen         = "workspace.ide.open"
	ActionWorkspaceTerminalOpen    = "workspace.terminal.open"
	ActionWorkspaceTerminalClose   = "workspace.terminal.close"
)

// Target types recorded alongside an action.
const (
	TargetProject  = "project"
	TargetUser     = "user"
	TargetSecret   = "secret"
	TargetChat     = "chat"
	TargetSchedule = "schedule"
	TargetAgent    = "agent"
	TargetFile     = "file"
	TargetSession  = "session"
	TargetServer   = "server"
	TargetSnapshot = "snapshot"
	// TargetMCPServer is one entry in the MCP registry, named by its
	// registry name — or, for a per-project override, by the project id.
	TargetMCPServer = "mcp-server"
	// TargetProvider is one entry in the free-tier provider pool, named by
	// its registry id. An empty id means the pool's own policy rather than
	// any single provider.
	TargetProvider = "provider"

	// TargetAgentEndpoint is one third-party agent endpoint profile, named
	// by its register id.
	TargetAgentEndpoint = "agent-endpoint"
)
