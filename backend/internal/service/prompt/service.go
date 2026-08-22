package prompt

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/service/audit"
	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
	"github.com/futrx-com/remote.futrx.com/internal/service/runhub"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

type ChatEvent = servicechat.Event
type ChatMeta = servicechat.Meta

type TmuxClient interface {
	Cwd(session string) (string, error)
}

// ProjectResolver decouples runner from project service internals. Lets tests
// stub project lookup/start without pulling in HTTP or persistence.
type ProjectResolver interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	Start(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	ListSecrets(ctx context.Context, id serviceproject.ID) ([]serviceproject.Secret, error)
}

type agentBrowserActivityRecorder interface {
	TouchAgentBrowserActivity(ctx context.Context, id serviceproject.ID)
}

var ErrPromptAlreadyRunning = errors.New("a previous prompt is still running")

type Actor struct {
	Email string
	// Sub is the OAuth subject of the session that started the run, when it
	// had one. User settings are keyed by subject first and email second, so
	// resolving a personal preference needs both halves of the identity.
	Sub     string
	IsAdmin bool
}

type StartInput struct {
	ChatID          servicechat.ID
	Prompt          string
	Actor           Actor
	ScheduledTaskID string
	ScheduledRunID  string
	ParentContext   context.Context
	// Synthetic labels a prompt the platform issued on the operator's behalf
	// rather than one a human typed — see servicechat.SyntheticAutopilot and
	// SyntheticAutoTest. It travels onto the persisted user event, onto the
	// audit entry, and back out on the run's outcome so the post-run driver
	// can tell its own follow-ups apart from real work.
	Synthetic string
}

type RunResult struct {
	Output string
	Err    error
}

type RunHandle struct {
	ID   uint64
	Done <-chan RunResult
}

type ScheduleToolRequest struct {
	Actor           Actor
	ChatID          servicechat.ID
	ProjectID       serviceproject.ID
	ScheduledTaskID string
	ScheduledRunID  string
}

type ScheduleToolAccess struct {
	APIURL string
	Token  string
	Revoke func()
}

type ScheduleToolIssuer interface {
	IssueScheduleTool(context.Context, ScheduleToolRequest) (ScheduleToolAccess, error)
}

// RunOutcome describes a settled agent run. Observers use it for out-of-band
// reporting such as outbound notifications; it never affects the run itself.
type RunOutcome struct {
	ChatID servicechat.ID
	RunID  uint64
	// Output is the concatenated assistant text produced by the run.
	Output    string
	Err       error
	Cancelled bool
	// ScheduledTaskID is set when the run was injected by the scheduler, so
	// observers can leave scheduled reporting to the schedule service.
	ScheduledTaskID string
	// Synthetic carries the label the run was started with, empty for a
	// human prompt.
	Synthetic string
}

// RunObserver receives out-of-band run signals. Implementations must not block:
// they are called on the run goroutine and on the provider event path.
type RunObserver interface {
	// RunSettled fires exactly once per run, after the provider returns.
	RunSettled(ctx context.Context, outcome RunOutcome)
	// RunToolStarted fires for every tool call the provider begins. Observers
	// decide which tool names mean the run is waiting on a human.
	RunToolStarted(ctx context.Context, chatID servicechat.ID, toolName string)
}

// UsageRecorder receives one entry per completed agent run. It is the only
// thing the prompt service knows about token accounting; pricing, storage and
// aggregation all live in the usage service.
type UsageRecorder interface {
	RecordRun(ctx context.Context, event serviceusage.RunEvent)
}

type Option func(*Service)

func WithScheduleToolIssuer(issuer ScheduleToolIssuer) Option {
	return func(service *Service) {
		service.scheduleTools = issuer
	}
}

// WithRunObserver installs an out-of-band observer of run lifecycle signals.
// Observers accumulate: notifications and the post-run driver both watch the
// same runs, and neither knows about the other.
func WithRunObserver(observer RunObserver) Option {
	return func(service *Service) {
		if observer != nil {
			service.observers = append(service.observers, observer)
		}
	}
}

// ReplyPreferenceResolver renders the platform's reply-preference preamble
// for one run. It is the second of the two injection channels — the first is
// the managed block in the project's workspace instructions file — and exists
// because a provider reads its system prompt before it reads any file.
//
// An empty result means "inject nothing", which is the default deployment.
type ReplyPreferenceResolver interface {
	RunPreamble(ctx context.Context, email, sub, projectID string) string
}

// WithReplyPreferences installs the reply-preference resolver. Without it runs
// carry no preference preamble.
func WithReplyPreferences(resolver ReplyPreferenceResolver) Option {
	return func(service *Service) {
		service.replyPrefs = resolver
	}
}

func WithUsageRecorder(recorder UsageRecorder) Option {
	return func(service *Service) {
		service.usage = recorder
	}
}

// ModelRouter decides which provider and model answer one turn. It is the
// prompt service's only view of automatic routing: the policy, the rule
// vocabulary, and the fallback logic all live in internal/service/routing.
//
// A nil router is today's behaviour exactly — the chat's own provider and
// model run every turn.
type ModelRouter interface {
	Route(ctx context.Context, input servicerouting.Input) servicerouting.Decision
}

// WithModelRouter installs the automatic model router.
func WithModelRouter(router ModelRouter) Option {
	return func(service *Service) {
		service.router = router
	}
}

// AgentEndpoints resolves the third-party endpoint a chat is pointed at into
// the environment and CLI arguments one run needs. It is the prompt service's
// only view of that register: which vendors are configured, which vault key
// each uses, and how each CLI's compatibility mode is spelled all live in
// internal/service/agentendpoints.
//
// A nil resolver is today's behaviour exactly — no chat can be pointed
// anywhere, so every run reaches the vendor the CLI is logged in to.
type AgentEndpoints interface {
	// RuntimeFor resolves one profile for one run. The model is the chat's
	// own choice; the register substitutes its default when the profile does
	// not offer that id. An error here fails the run before the CLI starts.
	RuntimeFor(ctx context.Context, endpointID, model string) (agent.Endpoint, error)
}

// WithAgentEndpoints installs the third-party endpoint resolver.
func WithAgentEndpoints(endpoints AgentEndpoints) Option {
	return func(service *Service) {
		service.endpoints = endpoints
	}
}

// WithAudit records the start and cancellation of every agent run.
func WithAudit(recorder audit.Recorder) Option {
	return func(service *Service) {
		service.audit = audit.RecorderOrNop(recorder)
	}
}

type Service struct {
	store         servicechat.Repository
	tmux          TmuxClient
	projects      ProjectResolver
	hub           *runhub.Hub
	agents        *agent.Registry
	scheduleTools ScheduleToolIssuer
	observers     []RunObserver
	usage         UsageRecorder
	audit         audit.Recorder
	replyPrefs    ReplyPreferenceResolver
	router        ModelRouter
	endpoints     AgentEndpoints
	direct        DirectResponder
}

func New(
	store servicechat.Repository,
	tmux TmuxClient,
	projects ProjectResolver,
	hub *runhub.Hub,
	agents *agent.Registry,
	options ...Option,
) *Service {
	if hub == nil {
		hub = runhub.New(store)
	}
	service := &Service{
		store:    store,
		tmux:     tmux,
		projects: projects,
		hub:      hub,
		agents:   agents,
		audit:    audit.Nop{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service
}

func (rnr *Service) StartPrompt(id servicechat.ID, prompt string, emitTransient func(ChatEvent)) {
	_, _ = rnr.Start(StartInput{ChatID: id, Prompt: prompt}, emitTransient)
}

func (rnr *Service) Start(input StartInput, emitTransient func(ChatEvent)) (RunHandle, error) {
	if emitTransient == nil {
		emitTransient = func(ChatEvent) {}
	}
	parentCtx := input.ParentContext
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)
	runID, ok := rnr.hub.StartRun(input.ChatID, cancel)
	if !ok {
		cancel()
		rnr.recordRun(parentCtx, audit.ActionAgentRunStart, input, ErrPromptAlreadyRunning)
		emitTransient(ChatEvent{
			T: time.Now().UnixMilli(), Type: "error",
			Message: "a previous prompt is still running — cancel first",
		})
		return RunHandle{}, ErrPromptAlreadyRunning
	}
	rnr.recordRun(parentCtx, audit.ActionAgentRunStart, input, nil)

	done := make(chan RunResult, 1)
	ledgerRunID := newLedgerRunID()
	go func() {
		defer close(done)
		defer rnr.hub.FinishRun(input.ChatID, runID)
		var output strings.Builder
		err := rnr.runPromptAs(
			ctx,
			input,
			ledgerRunID,
			func(ev ChatEvent) {
				rnr.hub.Emit(input.ChatID, ev)
				if ev.Type == "assistant_text" {
					output.WriteString(ev.Text)
				}
			},
			emitTransient,
		)
		rnr.observeSettled(parentCtx, RunOutcome{
			ChatID:          input.ChatID,
			RunID:           runID,
			Output:          output.String(),
			Err:             err,
			Cancelled:       errors.Is(ctx.Err(), context.Canceled),
			ScheduledTaskID: input.ScheduledTaskID,
			Synthetic:       input.Synthetic,
		})
		done <- RunResult{Output: output.String(), Err: err}
	}()
	return RunHandle{ID: runID, Done: done}, nil
}

// observeSettled reports a settled run to every installed observer. The
// observer's own context governs its work, so a cancelled run still gets
// reported.
func (rnr *Service) observeSettled(ctx context.Context, outcome RunOutcome) {
	if len(rnr.observers) == 0 {
		return
	}
	if ctx == nil || ctx.Err() != nil {
		ctx = context.Background()
	}
	for _, observer := range rnr.observers {
		observer.RunSettled(ctx, outcome)
	}
}

func (rnr *Service) CancelPrompt(ctx context.Context, id servicechat.ID) bool {
	canceled := rnr.hub.CancelRun(id)
	if rnr.audit != nil {
		rnr.audit.Record(ctx, audit.Result(
			audit.ActionAgentRunCancel,
			audit.Target{Type: audit.TargetChat, ID: string(id)},
			audit.Meta{"canceled": canceled},
			nil,
		))
	}
	return canceled
}

// recordRun writes one agent-run line. The provider and project come from the
// chat record, so a run is attributable even though the audit log never sees
// the prompt text.
func (rnr *Service) recordRun(ctx context.Context, action string, input StartInput, err error) {
	if rnr.audit == nil {
		return
	}
	meta := audit.Meta{"chatId": string(input.ChatID)}
	if chat, chatErr := rnr.store.Get(ctx, input.ChatID); chatErr == nil {
		meta["provider"] = string(providerIDFromChatProvider(chat.Provider))
		if chat.ProjectID != "" {
			meta["projectId"] = string(chat.ProjectID)
		}
		// Which third-party endpoint answered is the one fact that makes a
		// run attributable to a vendor other than the one the provider name
		// implies. The endpoint id is a handle, never a key.
		if endpointID := servicechat.NormalizeEndpointID(chat.EndpointID); endpointID != "" {
			meta["endpointId"] = endpointID
		}
	}
	if input.ScheduledTaskID != "" {
		meta["scheduledTaskId"] = input.ScheduledTaskID
		meta["scheduledRunId"] = input.ScheduledRunID
	}
	if synthetic := servicechat.NormalizeSynthetic(input.Synthetic); synthetic != "" {
		meta["synthetic"] = synthetic
	}
	entry := audit.Result(
		action,
		audit.Target{Type: audit.TargetChat, ID: string(input.ChatID)},
		meta,
		err,
	)
	if input.Actor.Email != "" {
		entry.Actor = audit.Actor{
			Email:   audit.NormalizeActorEmail(input.Actor.Email),
			IsAdmin: input.Actor.IsAdmin,
		}
	}
	rnr.audit.Record(ctx, entry)
}

func (rnr *Service) runPrompt(
	ctx context.Context,
	id servicechat.ID,
	prompt string,
	emit func(ChatEvent),
	emitTransient func(ChatEvent),
) error {
	return rnr.runPromptAs(
		ctx,
		StartInput{ChatID: id, Prompt: prompt},
		newLedgerRunID(),
		emit,
		emitTransient,
	)
}

func (rnr *Service) runPromptAs(
	ctx context.Context,
	input StartInput,
	ledgerRunID string,
	emit func(ChatEvent),
	emitTransient func(ChatEvent),
) error {
	id := input.ChatID
	prompt := input.Prompt
	meta, err := rnr.store.Get(ctx, id)
	if err != nil {
		emitTransient(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: err.Error()})
		return err
	}

	// Auto-title from first user prompt if still default.
	if meta.Title == "" || meta.Title == "New chat" {
		_, _ = rnr.store.Update(ctx, id, func(m *ChatMeta) {
			m.Title = servicechat.TitleFromPrompt(prompt)
		})
	}

	// Resolve a fresh cwd: live tmux pane_current_path if linked, else stored.
	cwd := meta.Cwd
	if meta.TmuxSession != "" {
		if c, err := rnr.tmux.Cwd(meta.TmuxSession); err == nil && c != "" {
			cwd = c
		}
	}
	if cwd == "" {
		cwd = os.Getenv("HOME")
		if cwd == "" {
			cwd = "/root"
		}
	}

	priorEvents, _ := rnr.store.ReadEvents(ctx, id)

	// A chat pointed at a completion-API model never reaches a container.
	// This is checked before anything else in the run path — the endpoint
	// register, the model router, the skill selection, the browser and
	// schedule grants all describe an agent run, and none of them means
	// anything to a model that has no tools.
	if meta.DirectModel.Set() {
		return rnr.answerDirectly(ctx, id, meta, prompt, input.Actor, priorEvents, emit)
	}

	// A chat pointed at a third-party endpoint is resolved first, because the
	// endpoint decides which CLI answers. Resolving it can fail — a deleted
	// profile, one switched off, a vault key nobody set — and every one of
	// those must stop the turn here with a sentence about the configuration
	// rather than reach a vendor and come back as an opaque 401.
	runEndpoint, err := rnr.resolveEndpoint(ctx, meta)
	if err != nil {
		emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: err.Error()})
		return err
	}

	// Automatic model routing is applied here, before anything else reads the
	// provider: the decision can change which agent answers, and the provider
	// selects the resume session, the skill trigger syntax, and the adapter
	// this turn runs through. Without a router the decision repeats the
	// chat's own provider and model, which is what every turn did before
	// routing existed.
	//
	// An endpoint wins over routing rather than extending it. The two would
	// otherwise have to agree about something they cannot: a routing rule
	// names a model from the platform's own catalog, and an endpoint offers a
	// vendor's model ids under a specific CLI, so a routed decision landing
	// on a chat pinned to GLM could only ever produce a model name the
	// endpoint has never heard of. Pointing a chat at an endpoint is
	// therefore a pin, exactly like choosing a model by hand.
	routed := ownDecision(meta)
	if runEndpoint != nil {
		routed.Provider = string(runEndpoint.CLI)
	} else {
		routed = rnr.route(ctx, meta, input, prompt)
	}
	providerID := providerIDFromChatProvider(servicechat.Provider(routed.Provider))
	runModel := routed.Model
	if runEndpoint != nil {
		// The register resolved which of the endpoint's models this turn asks
		// for; the chat's stored model is only a request.
		runModel = runEndpoint.Model
	}

	// Persist the user message before spawning the selected agent. A
	// synthetic label rides along so the transcript shows who asked, and the
	// routing block records which model answered and why.
	emit(ChatEvent{
		T:         time.Now().UnixMilli(),
		Type:      "user",
		Text:      prompt,
		Synthetic: servicechat.NormalizeSynthetic(input.Synthetic),
		Routing:   routingEvent(routed),
	})

	promptSkills := meta.SelectedSkills
	if input.ScheduledTaskID != "" && !hasScheduledTasksSkill(promptSkills) {
		promptSkills = append(
			append([]servicechat.SkillRef(nil), promptSkills...),
			servicechat.SkillRef{
				Name:     "Scheduled Tasks",
				Command:  scheduledTasksSkillName,
				Provider: servicechat.Provider(providerID),
				Source:   "remote",
			},
		)
	}
	resumeID := sessionIDForProvider(meta, providerID)
	replyPreference := rnr.replyPreference(ctx, input.Actor, string(meta.ProjectID))
	effectivePrompt := promptWithReplyPreference(replyPreference, promptForMode(meta.Mode, prompt))
	enableBrowser := hasBrowserSkill(meta.SelectedSkills)
	if enableBrowser && meta.ProjectID != "" {
		stopBrowserKeepalive := rnr.keepAgentBrowserActivity(ctx, serviceproject.ID(meta.ProjectID))
		defer stopBrowserKeepalive()
	}
	if resumeID == "" {
		effectivePrompt = promptWithVisibleHistory(priorEvents, effectivePrompt)
	}
	effectivePrompt = promptWithSelectedSkills(providerID, promptSkills, effectivePrompt)

	provider := rnr.agents.Lookup(providerID)
	if provider == nil {
		err := errors.New(string(providerID) + " provider not configured")
		emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: err.Error()})
		return err
	}

	enableScheduleTools := hasScheduledTasksSkill(meta.SelectedSkills) || input.ScheduledTaskID != ""
	runtimeEnv := map[string]string(nil)
	if enableScheduleTools {
		if meta.ProjectID == "" {
			err := errors.New("scheduled tasks are only available in project chats")
			emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: err.Error()})
			return err
		}
		if rnr.scheduleTools == nil {
			err := errors.New("scheduled task tools are unavailable")
			emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: err.Error()})
			return err
		}
		access, accessErr := rnr.scheduleTools.IssueScheduleTool(ctx, ScheduleToolRequest{
			Actor:           input.Actor,
			ChatID:          id,
			ProjectID:       serviceproject.ID(meta.ProjectID),
			ScheduledTaskID: input.ScheduledTaskID,
			ScheduledRunID:  input.ScheduledRunID,
		})
		if accessErr != nil {
			emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: accessErr.Error()})
			return accessErr
		}
		if access.Revoke != nil {
			defer access.Revoke()
		}
		runtimeEnv = map[string]string{
			"REMOTE_SCHEDULE_API":   access.APIURL,
			"REMOTE_SCHEDULE_GRANT": access.Token,
		}
	}

	ledger := ledgerRun{
		runID:       ledgerRunID,
		chatID:      id,
		projectID:   string(meta.ProjectID),
		userEmail:   input.Actor.Email,
		provider:    providerID,
		model:       runModel,
		scheduled:   input.ScheduledTaskID != "",
		routedBy:    routedBy(routed),
		routedModel: routedModel(routed),
	}

	run := func(runPrompt, runResumeID string) error {
		return provider.Run(ctx, agent.RunRequest{
			Provider:       providerID,
			ConversationID: string(id),
			Prompt:         runPrompt,
			Cwd:            cwd,
			Model:          runModel,
			Mode:           meta.Mode,
			ResumeID:       runResumeID,
			ProjectID:      string(meta.ProjectID),
			Fork:           meta.ForkPending,
			Preferences: agent.RunPreferences{
				ReasoningEffort: agent.ReasoningEffort(routed.ReasoningEffort),
				ServiceTier:     agent.ServiceTier(meta.ServiceTier),
			},
			EnableBrowser:       enableBrowser,
			EnableScheduleTools: enableScheduleTools,
			RuntimeEnv:          runtimeEnv,
			Endpoint:            runEndpoint,
		}, func(ev agent.Event) {
			rnr.emitAgentEvent(ctx, id, ev, emit)
			rnr.recordRunUsage(ctx, ledger, ev)
		})
	}

	err = run(effectivePrompt, resumeID)
	if errors.Is(err, agent.ErrSessionNotFound) && resumeID != "" {
		_, _ = rnr.store.Update(ctx, id, func(m *ChatMeta) {
			clearSessionIDForProvider(m, providerID)
			m.ForkPending = false
		})
		emit(ChatEvent{T: time.Now().UnixMilli(), Type: "system", Subtype: "session_recovered"})
		freshPrompt := promptWithReplyPreference(replyPreference, promptForMode(meta.Mode, prompt))
		freshPrompt = promptWithVisibleHistory(priorEvents, freshPrompt)
		freshPrompt = promptWithSelectedSkills(providerID, promptSkills, freshPrompt)
		err = run(freshPrompt, "")
	}
	if err != nil && !errors.Is(err, agent.ErrRunFailed) {
		emit(ChatEvent{T: time.Now().UnixMilli(), Type: "error", Message: string(providerID) + " exit: " + err.Error()})
	}
	return err
}

// route asks the installed router which provider and model should answer this
// turn. A nil router, a chat pinned to its own model, or a policy that is
// switched off all produce the chat's own choice.
func (rnr *Service) route(
	ctx context.Context,
	meta ChatMeta,
	input StartInput,
	prompt string,
) servicerouting.Decision {
	own := ownDecision(meta)
	if rnr.router == nil {
		return own
	}
	decision := rnr.router.Route(ctx, servicerouting.Input{
		Pinned: servicechat.NormalizeModelPolicy(meta.ModelPolicy) !=
			servicechat.ModelPolicyAuto,
		Provider:        own.Provider,
		Model:           own.Model,
		ReasoningEffort: own.ReasoningEffort,
		Prompt:          prompt,
		Mode:            meta.Mode,
		Synthetic:       servicechat.NormalizeSynthetic(input.Synthetic),
		ProjectID:       string(meta.ProjectID),
		ProjectSlug:     rnr.projectSlug(ctx, meta.ProjectID),
		Skills:          skillTriggerNames(meta.SelectedSkills),
	})
	// A router that answered with nothing must never blank the run's model.
	if strings.TrimSpace(decision.Provider) == "" {
		return own
	}
	return decision
}

// ownDecision is the chat's own choice, expressed as a decision that routed
// nowhere. It is what a deployment without a router produces, and what a chat
// pinned to an endpoint produces.
func ownDecision(meta ChatMeta) servicerouting.Decision {
	return servicerouting.Decision{
		Provider:        string(servicechat.NormalizeProvider(meta.Provider)),
		Model:           meta.Model,
		ReasoningEffort: meta.ReasoningEffort,
	}
}

// resolveEndpoint turns the chat's stored endpoint handle into the rendered
// configuration one run needs, or nil when the chat names none — which is
// every chat by default and the behaviour the platform had before the
// register existed.
//
// The failures are all configuration failures and all of them are worth a
// sentence: a chat pointed at a profile an admin deleted, a profile switched
// off, or a Secrets-vault key nobody has set yet. Reporting them here costs
// the operator nothing; discovering them from a vendor's error body costs
// them the prompt.
func (rnr *Service) resolveEndpoint(ctx context.Context, meta ChatMeta) (*agent.Endpoint, error) {
	endpointID := servicechat.NormalizeEndpointID(meta.EndpointID)
	if endpointID == "" {
		return nil, nil
	}
	if rnr.endpoints == nil {
		return nil, errors.New(
			"this chat is pointed at agent endpoint " + endpointID +
				", which this deployment does not have configured",
		)
	}
	resolved, err := rnr.endpoints.RuntimeFor(ctx, endpointID, meta.Model)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// projectSlug resolves the readable name a `projectIs` rule may be written
// against. A project the resolver cannot read simply leaves the slug empty,
// so the rule falls back to matching on the id.
func (rnr *Service) projectSlug(ctx context.Context, projectID servicechat.ProjectID) string {
	if projectID == "" || rnr.projects == nil {
		return ""
	}
	meta, err := rnr.projects.Get(ctx, serviceproject.ID(projectID))
	if err != nil {
		return ""
	}
	return meta.Slug
}

// routingEvent renders a decision for the transcript. An unrouted turn records
// nothing, so a pinned chat's history looks exactly as it always did.
func routingEvent(decision servicerouting.Decision) *servicechat.EventRouting {
	if !decision.Routed {
		return nil
	}
	return &servicechat.EventRouting{
		Provider: decision.Provider,
		Model:    decision.Model,
		RuleID:   decision.RuleID,
		Rule:     decision.RuleNote,
		Reason:   decision.Reason,
	}
}

// routedBy names what chose this run's model, for the ledger: the rule id, the
// heuristic id, or "default" when routing fell through to the policy default.
// Empty means the run was never routed.
func routedBy(decision servicerouting.Decision) string {
	if !decision.Routed {
		return ""
	}
	if decision.RuleID != "" {
		return decision.RuleID
	}
	return servicerouting.RoutedByDefault
}

// routedModel is the destination the policy names, not the model id the
// provider later reports. The savings report compares it against the policy's
// cheap and expensive poles, which are written in the same vocabulary.
func routedModel(decision servicerouting.Decision) string {
	if !decision.Routed {
		return ""
	}
	return servicerouting.ModelRef{
		Provider: decision.Provider,
		Model:    decision.Model,
	}.Key()
}

// skillTriggerNames is the selected-skill list a `skillSelected` rule matches
// against, in the same normalized form the prompt preamble uses.
func skillTriggerNames(skills []servicechat.SkillRef) []string {
	if len(skills) == 0 {
		return nil
	}
	names := make([]string, 0, len(skills))
	for _, skill := range skills {
		name := skillTriggerName(skill.Command)
		if name == "" {
			name = skillTriggerName(skill.Name)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func clearSessionIDForProvider(meta *ChatMeta, provider agent.ProviderID) {
	switch provider {
	case agent.ProviderCodex:
		meta.CodexSessionID = ""
	case agent.ProviderKimi:
		meta.KimiSessionID = ""
	case agent.ProviderAntigravity:
		meta.AntigravitySessionID = ""
	default:
		meta.ClaudeSessionID = ""
	}
}

func (rnr *Service) keepAgentBrowserActivity(ctx context.Context, projectID serviceproject.ID) func() {
	recorder, ok := rnr.projects.(agentBrowserActivityRecorder)
	if !ok || recorder == nil {
		return func() {}
	}
	recorder.TouchAgentBrowserActivity(ctx, projectID)
	keepaliveCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-keepaliveCtx.Done():
				return
			case <-ticker.C:
				recorder.TouchAgentBrowserActivity(keepaliveCtx, projectID)
			}
		}
	}()
	return cancel
}

func providerIDFromChatProvider(provider servicechat.Provider) agent.ProviderID {
	switch servicechat.NormalizeProvider(provider) {
	case servicechat.ProviderCodex:
		return agent.ProviderCodex
	case servicechat.ProviderKimi:
		return agent.ProviderKimi
	case servicechat.ProviderAntigravity:
		return agent.ProviderAntigravity
	default:
		return agent.ProviderClaude
	}
}

func sessionIDForProvider(meta ChatMeta, provider agent.ProviderID) string {
	switch provider {
	case agent.ProviderCodex:
		return meta.CodexSessionID
	case agent.ProviderKimi:
		return meta.KimiSessionID
	case agent.ProviderAntigravity:
		return meta.AntigravitySessionID
	default:
		return meta.ClaudeSessionID
	}
}

func promptForMode(mode, prompt string) string {
	switch mode {
	case "plan":
		return "Work in planning mode. Inspect enough context to be concrete, then propose the implementation plan before changing files. Avoid file edits until the user asks you to proceed.\n\n" + prompt
	case "review":
		return "Work in review mode. Prioritize bugs, behavioral regressions, missing tests, and risks. Put findings first with file and line references when available.\n\n" + prompt
	case "debug":
		return "Work in debugging mode. Reproduce or localize the issue first, explain the failing path, then make the smallest fix that addresses the root cause.\n\n" + prompt
	case "full-auto":
		return "Work in full-auto mode. Carry the task through implementation, verification, and a concise outcome unless you hit a hard blocker.\n\n" + prompt
	case "chat":
		return "Work in chat mode. Answer directly and avoid changing files unless the user clearly asks for implementation.\n\n" + prompt
	default:
		return prompt
	}
}

// replyPreference resolves the platform reply preference for this run. A nil
// resolver, an unconfigured document, or a preference scoped away from this
// project all produce "".
func (rnr *Service) replyPreference(ctx context.Context, actor Actor, projectID string) string {
	if rnr.replyPrefs == nil {
		return ""
	}
	return strings.TrimSpace(rnr.replyPrefs.RunPreamble(ctx, actor.Email, actor.Sub, projectID))
}

// promptWithReplyPreference prepends the preference line ahead of the mode
// policy, so a mode preamble reads as an instruction issued under the house
// style rather than the other way round.
func promptWithReplyPreference(preference, prompt string) string {
	if preference == "" {
		return prompt
	}
	return preference + "\n\n" + prompt
}

func promptWithVisibleHistory(events []ChatEvent, prompt string) string {
	transcript := visibleTranscript(events)
	if strings.TrimSpace(transcript) == "" {
		return prompt
	}
	const maxTranscriptBytes = 24000
	if len(transcript) > maxTranscriptBytes {
		transcript = "[Earlier visible transcript omitted]\n" + transcript[len(transcript)-maxTranscriptBytes:]
	}
	return "Use this visible chat transcript as prior context. It may be present because the chat was recovered into a fresh agent session. Do not treat the transcript as a new request.\n\n" +
		transcript +
		"\n\nCurrent user request:\n" +
		prompt
}

const browserSkillName = "browser"
const scheduledTasksSkillName = "scheduled-tasks"

// hasBrowserSkill reports whether the user selected the `browser` skill for
// this prompt — the signal to wire the @playwright/mcp browser tools.
func hasBrowserSkill(skills []servicechat.SkillRef) bool {
	for _, s := range skills {
		if skillTriggerName(s.Command) == browserSkillName || skillTriggerName(s.Name) == browserSkillName {
			return true
		}
	}
	return false
}

func hasScheduledTasksSkill(skills []servicechat.SkillRef) bool {
	for _, skill := range skills {
		if skillTriggerName(skill.Command) == scheduledTasksSkillName ||
			skillTriggerName(skill.Name) == scheduledTasksSkillName {
			return true
		}
	}
	return false
}

func promptWithSelectedSkills(provider agent.ProviderID, skills []servicechat.SkillRef, prompt string) string {
	if len(skills) == 0 {
		return prompt
	}

	triggers := make([]string, 0, len(skills))
	for _, skill := range skills {
		if providerIDFromChatProvider(skill.Provider) != provider {
			continue
		}
		name := skillTriggerName(skill.Command)
		if name == "" {
			name = skillTriggerName(skill.Name)
		}
		if name == "" {
			continue
		}

		switch provider {
		case agent.ProviderClaude:
			triggers = append(triggers, "/"+name)
		case agent.ProviderCodex:
			triggers = append(triggers, "$"+name)
		case agent.ProviderKimi, agent.ProviderAntigravity:
			if name == scheduledTasksSkillName {
				triggers = append(
					triggers,
					"/workspace/.agents/skills/scheduled-tasks/SKILL.md",
				)
			}
		}
	}
	if len(triggers) == 0 {
		return prompt
	}

	switch provider {
	case agent.ProviderClaude:
		return strings.Join(triggers, "\n") + "\n\n" + prompt
	case agent.ProviderCodex:
		return "Use these Codex skills for this request: " + strings.Join(triggers, " ") + "\n\n" + prompt
	case agent.ProviderKimi, agent.ProviderAntigravity:
		return "Read and follow the selected skill instructions at " +
			strings.Join(triggers, ", ") + ".\n\n" + prompt
	default:
		return prompt
	}
}

func skillTriggerName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimLeft(value, "/$")
	if value == "" {
		return ""
	}
	parts := strings.Fields(value)
	if len(parts) <= 1 {
		return value
	}
	return strings.Join(parts, "-")
}

func visibleTranscript(events []ChatEvent) string {
	var out strings.Builder
	var assistant strings.Builder

	flushAssistant := func() {
		text := strings.TrimSpace(assistant.String())
		if text == "" {
			assistant.Reset()
			return
		}
		out.WriteString("Assistant:\n")
		out.WriteString(text)
		out.WriteString("\n\n")
		assistant.Reset()
	}

	for _, ev := range events {
		switch ev.Type {
		case "user":
			flushAssistant()
			out.WriteString("User:\n")
			out.WriteString(strings.TrimSpace(ev.Text))
			out.WriteString("\n\n")
		case "assistant_text":
			assistant.WriteString(ev.Text)
		case "complete", "error":
			flushAssistant()
		}
	}
	flushAssistant()
	return out.String()
}
