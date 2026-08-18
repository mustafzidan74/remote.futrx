import { requestJson } from "../apiRequest";
import type {
  ClientMessageResult,
  ProjectPortal,
  UpdateProjectPortalInput,
} from "../../models/project";
import { API_ROUTES } from "../../config/routes";

export const projectPortalApi = {
  getPortal: (id: string) =>
    requestJson<ProjectPortal>("GET", API_ROUTES.projects.portal(id)),

  savePortal: (id: string, body: UpdateProjectPortalInput) =>
    requestJson<ProjectPortal>("PUT", API_ROUTES.projects.portal(id), body),

  /**
   * Reports whether any notification sink could carry a client message, so
   * the panel hides the send button instead of offering a guaranteed failure.
   */
  getClientMessageSinks: (id: string) =>
    requestJson<ClientMessageResult>("GET", API_ROUTES.projects.clientMessage(id)),

  /**
   * Hands an already-resolved message to the configured sinks. The server
   * treats the text as opaque: the placeholders were filled in here.
   */
  sendClientMessage: (id: string, text: string, url?: string) =>
    requestJson<ClientMessageResult>("POST", API_ROUTES.projects.clientMessage(id), {
      text,
      ...(url ? { url } : {}),
    }),
};
