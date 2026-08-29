package httphandlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	servicelighthouse "github.com/futrx-com/remote.futrx.com/internal/service/lighthouse"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// handleLighthouse serves /api/projects/{id}/lighthouse and everything under
// it. Project membership was already established by HandleResource.
//
//	GET    /lighthouse            the run history, whether one is in flight,
//	                              and whether the container has the CLI
//	POST   /lighthouse            audit these pages
//	POST   /lighthouse/install    add the CLI to a container that predates it
//	DELETE /lighthouse/{runID}    drop one run
func (h *ProjectHandler) handleLighthouse(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	parts []string,
	email string,
) {
	if h.lighthouse == nil || !h.lighthouse.Available() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicelighthouse.ErrUnavailable.Error())
		return
	}

	rest := ""
	if len(parts) >= 3 {
		rest = strings.Trim(strings.Join(parts[2:], "/"), "/")
	}

	switch {
	case rest == "":
		h.lighthouseRoot(w, r, projectID, email)
	case rest == "install":
		h.lighthouseInstall(w, r, projectID, email)
	default:
		h.lighthouseDelete(w, r, projectID, rest)
	}
}

func (h *ProjectHandler) lighthouseRoot(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	email string,
) {
	switch r.Method {
	case http.MethodGet:
		overview, err := h.lighthouse.Overview(r.Context(), projectID)
		if err != nil {
			sendLighthouseError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, overview)
	case http.MethodPost:
		var body servicelighthouse.RunInput
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		run, err := h.lighthouse.Start(r.Context(), projectID, body, email)
		if err != nil {
			sendLighthouseError(w, err)
			return
		}
		// 202: the run exists and is real, but the pages are still being
		// audited. The panel polls until it stops running.
		httptransport.SendJSON(w, http.StatusAccepted, run)
	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *ProjectHandler) lighthouseInstall(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	email string,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	// Synchronous on purpose: it is under a minute and the operator pressed a
	// button expecting it to be done when the request answers.
	if err := h.lighthouse.Install(r.Context(), projectID, email); err != nil {
		sendLighthouseError(w, err)
		return
	}
	overview, err := h.lighthouse.Overview(r.Context(), projectID)
	if err != nil {
		sendLighthouseError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, overview)
}

func (h *ProjectHandler) lighthouseDelete(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	rawID string,
) {
	if r.Method != http.MethodDelete {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if rawID == "" || strings.ContainsAny(rawID, `/\.`) {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid run id")
		return
	}
	if err := h.lighthouse.Delete(r.Context(), projectID, servicelighthouse.ID(rawID)); err != nil {
		sendLighthouseError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sendLighthouseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicelighthouse.ErrInvalidPort),
		errors.Is(err, servicelighthouse.ErrInvalidPath),
		errors.Is(err, servicelighthouse.ErrNoPaths),
		errors.Is(err, servicelighthouse.ErrTooManyPaths),
		errors.Is(err, serviceproject.ErrInvalidID):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicelighthouse.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	// Not-running, busy and missing-CLI are all "the platform is fine, the
	// project is not ready for this yet", which is a 409 the UI turns into a
	// sentence with a fix in it rather than an error toast.
	case errors.Is(err, servicelighthouse.ErrNotRunning),
		errors.Is(err, servicelighthouse.ErrBusy),
		errors.Is(err, servicelighthouse.ErrToolingMissing):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, servicelighthouse.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
