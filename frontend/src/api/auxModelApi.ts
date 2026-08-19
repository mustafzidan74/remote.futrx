import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type {
  AuxModelAvailability,
  AuxModelSettings,
  AuxModelTestResult,
  TranslationResult,
  TranslationTarget,
  UpdateAuxModelInput,
} from "../models/auxModel";

export const auxModelApi = {
  /** What the browser may know: which jobs are worth offering a button for. */
  availability: () =>
    requestJson<AuxModelAvailability>("GET", API_ROUTES.auxModel.config),

  settings: () => requestJson<AuxModelSettings>("GET", API_ROUTES.auxModel.settings),
  save: (input: UpdateAuxModelInput) =>
    requestJson<AuxModelSettings>("PUT", API_ROUTES.auxModel.settings, input),

  /** Runs one real completion and reports latency plus the model's answer. */
  test: () => requestJson<AuxModelTestResult>("POST", API_ROUTES.auxModel.test),

  translate: (text: string, target: TranslationTarget) =>
    requestJson<TranslationResult>("POST", API_ROUTES.auxModel.translate, { text, target }),
};
