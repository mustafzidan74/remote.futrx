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
	ActionSnapshotCreate    = "snapshot.create"
	ActionSnapshotRestore   = "snapshot.restore"
	ActionSnapshotDelete    = "snapshot.delete"

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
)
