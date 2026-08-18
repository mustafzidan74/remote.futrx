import { requestJson } from "../apiRequest";
import { API_ROUTES } from "../../config/routes";
import type {
  CreateScheduledTaskInput,
  ScheduledTask,
  ScheduleRunDiff,
  ScheduleRunRecord,
  UpdateScheduledTaskInput,
} from "../../models/schedule";

type ScheduledTaskWire = ScheduledTask & { lastRunError?: string };

export const chatScheduleApi = {
  fetchSchedules: (chatId: string) =>
    requestJson<ScheduledTaskWire[]>(
      "GET",
      API_ROUTES.chats.schedules(chatId)
    ).then((tasks) => tasks.map(normalizeScheduledTask)),

  createSchedule: (chatId: string, body: CreateScheduledTaskInput) =>
    requestJson<ScheduledTaskWire>(
      "POST",
      API_ROUTES.chats.schedules(chatId),
      body
    ).then(normalizeScheduledTask),

  updateSchedule: (id: string, body: UpdateScheduledTaskInput) =>
    requestJson<ScheduledTaskWire>(
      "PATCH",
      API_ROUTES.schedules.item(id),
      body
    ).then(normalizeScheduledTask),

  deleteSchedule: (id: string) =>
    requestJson<{ ok: boolean }>("DELETE", API_ROUTES.schedules.item(id)),

  fetchScheduleHistory: (id: string) =>
    requestJson<ScheduleRunRecord[]>("GET", API_ROUTES.schedules.history(id)),

  fetchScheduleRunDiff: (id: string, runId: string) =>
    requestJson<ScheduleRunDiff>("GET", API_ROUTES.schedules.runDiff(id, runId)),

  runSchedule: (id: string) =>
    requestJson<ScheduledTaskWire | { ok: boolean }>(
      "POST",
      API_ROUTES.schedules.run(id),
      {}
    ).then((result) => "id" in result ? normalizeScheduledTask(result) : result),
};

function normalizeScheduledTask(task: ScheduledTaskWire): ScheduledTask {
  const { lastRunError, ...normalized } = task;
  return {
    ...normalized,
    lastError: normalized.lastError || lastRunError,
  };
}
