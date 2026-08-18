import type {
  CreateScheduledTaskInput,
  ScheduleCondition,
} from "../../../models/schedule";

// A ready-made scheduled task. Everything a template pre-fills is editable in
// the create form before it is saved, so a template is a starting point, never
// a hidden contract.
export interface ScheduleTemplate {
  id: string;
  name: string;
  /** One line explaining when this job earns its slot. */
  blurb: string;
  taskName: string;
  cron: string;
  prompt: string;
  maxRuns?: number;
  condition?: ScheduleCondition;
  /** Skills the prompt asks the agent to select for the run. */
  skills: string[];
}

// The prompts deliberately end by asking for a verdict marker: it is what
// makes a template composable — a later task can gate on this one's result.
export const SCHEDULE_TEMPLATES: ScheduleTemplate[] = [
  {
    id: "weekly-site-audit",
    name: "Weekly site audit",
    blurb: "Crawls the preview site every Monday and reports regressions.",
    taskName: "Weekly site audit",
    cron: "0 7 * * 1",
    skills: ["Agent browser", "Scheduled Tasks"],
    condition: { kind: "weekdays", weekdays: [1] },
    prompt: [
      "Goal: audit this project's running site once a week.",
      "",
      "On each run:",
      "1. Open the project's preview URL with the agent browser.",
      "2. Check that the home page renders, the console is free of errors, and",
      "   the main navigation links resolve.",
      "3. Compare what you find against the previous audit in this chat and",
      "   report only what changed.",
      "",
      "Report the findings, then end with exactly one of:",
      "<<RESULT: OK>>   (nothing regressed)",
      "<<RESULT: ISSUES>>   (something needs a human)",
    ].join("\n"),
  },
  {
    id: "nightly-backup-verify",
    name: "Nightly backup verify",
    blurb: "Confirms last night's backup exists, is fresh, and restores.",
    taskName: "Nightly backup verify",
    cron: "30 3 * * *",
    skills: ["Scheduled Tasks"],
    condition: { kind: "commandExitCode", command: "test -d /workspace/backups", expect: 0 },
    prompt: [
      "Goal: verify that last night's backup is usable.",
      "",
      "On each run:",
      "1. List the newest backup artifact and report its size and timestamp.",
      "2. Fail loudly if the newest artifact is older than 26 hours or is",
      "   smaller than the one before it.",
      "3. Do a cheap integrity check (archive listing or checksum). Do not",
      "   restore over live data.",
      "",
      "End with exactly one of:",
      "<<RESULT: OK>>",
      "<<RESULT: STALE>>",
      "<<RESULT: FAILED>>",
    ].join("\n"),
  },
  {
    id: "dependency-update-test",
    name: "Dependency update + test",
    blurb: "Bumps dependencies weekly, runs the suite, and reverts on red.",
    taskName: "Dependency update + test",
    cron: "0 5 * * 2",
    skills: ["Scheduled Tasks"],
    condition: { kind: "commandExitCode", command: "git -C /workspace diff --quiet", expect: 0 },
    prompt: [
      "Goal: keep dependencies current without ever leaving the tree broken.",
      "",
      "On each run:",
      "1. Update the lockfile to the latest compatible versions (no major",
      "   bumps unless the changelog is clean).",
      "2. Install and run the full test suite.",
      "3. If the suite passes, commit the update with a short message listing",
      "   the packages that moved. If it fails, revert the lockfile and report",
      "   which package broke.",
      "",
      "End with exactly one of:",
      "<<RESULT: UPDATED>>",
      "<<RESULT: NO-CHANGES>>",
      "<<RESULT: FAILED>>",
    ].join("\n"),
  },
  {
    id: "weekly-cost-report",
    name: "Weekly cost report",
    blurb: "Summarises the week's agent spend and flags the outliers.",
    taskName: "Weekly cost report",
    cron: "0 9 * * 5",
    skills: ["Scheduled Tasks"],
    condition: { kind: "notIfRanWithin", minutes: 6 * 24 * 60 },
    prompt: [
      "Goal: report what this project spent on agent runs this week.",
      "",
      "On each run:",
      "1. Summarise the last seven days of usage: total tokens, estimated",
      "   cost, and the split by provider and model.",
      "2. Call out anything unusual — a single run above 10% of the week, a",
      "   provider that appeared for the first time, a doubling week over week.",
      "3. Suggest one concrete saving if there is an obvious one.",
      "",
      "End with the total, for example:",
      "<<RESULT: USD=12.40>>",
    ].join("\n"),
  },
];

export function findScheduleTemplate(id: string): ScheduleTemplate | undefined {
  return SCHEDULE_TEMPLATES.find((template) => template.id === id);
}

// templateToCreateInput turns a template into the create-form's starting
// values. The timezone comes from the browser so the pre-filled cron means
// what the user reads.
export function templateToCreateInput(
  template: ScheduleTemplate,
  timezone: string
): CreateScheduledTaskInput {
  return {
    name: template.taskName,
    prompt: template.prompt,
    kind: "cron",
    cron: template.cron,
    timezone: timezone || "UTC",
    maxRuns: template.maxRuns ?? 0,
    condition: template.condition ? { ...template.condition } : undefined,
  };
}

// browserTimezone is the IANA zone the user is reading the drawer in, falling
// back to UTC where Intl is unavailable.
export function browserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}
