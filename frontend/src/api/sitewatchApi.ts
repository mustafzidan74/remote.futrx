import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type {
  SiteCandidate,
  SiteCheckRecord,
  SiteCheckReport,
  SiteImportInput,
  SiteImportResult,
  WatchedSiteCollection,
  WatchedSiteInput,
  WatchedSiteView,
} from "../models/sitewatch";

export const sitewatchApi = {
  list: () => requestJson<WatchedSiteCollection>("GET", API_ROUTES.sitewatch.collection),
  create: (input: WatchedSiteInput) =>
    requestJson<WatchedSiteView>("POST", API_ROUTES.sitewatch.collection, input),
  update: (id: string, input: WatchedSiteInput) =>
    requestJson<WatchedSiteView>("PUT", API_ROUTES.sitewatch.item(id), input),
  remove: (id: string) => requestJson<void>("DELETE", API_ROUTES.sitewatch.item(id)),
  check: (id: string) => requestJson<SiteCheckReport>("POST", API_ROUTES.sitewatch.check(id)),
  history: (id: string) =>
    requestJson<{ checks: SiteCheckRecord[] }>("GET", API_ROUTES.sitewatch.history(id)),
  import: (input: SiteImportInput) =>
    requestJson<SiteImportResult>("POST", API_ROUTES.sitewatch.import, input),
  candidates: () =>
    requestJson<{ candidates: SiteCandidate[] }>("GET", API_ROUTES.sitewatch.candidates),
};
