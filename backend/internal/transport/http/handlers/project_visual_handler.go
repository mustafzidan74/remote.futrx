package httphandlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	servicevisualdiff "github.com/futrx-com/remote.futrx.com/internal/service/visualdiff"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// handleVisual serves /api/projects/{id}/visual and everything under it.
// Project membership was already established by HandleResource.
//
//	GET    /visual                  the baseline, the comparisons, and whether
//	                                a run is in flight
//	POST   /visual/baseline         photograph the pages and make them the
//	                                reference
//	POST   /visual/compare          re-photograph and diff against it
//	GET    /visual/images/{file}    one stored PNG
//	DELETE /visual/{comparisonID}   drop one comparison and its images
func (h *ProjectHandler) handleVisual(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	parts []string,
	email string,
) {
	if h.visual == nil || !h.visual.Available() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicevisualdiff.ErrUnavailable.Error())
		return
	}

	rest := ""
	if len(parts) >= 3 {
		rest = strings.Trim(strings.Join(parts[2:], "/"), "/")
	}

	switch {
	case rest == "":
		h.visualOverview(w, r, projectID)
	case rest == "baseline":
		h.visualBaseline(w, r, projectID, email)
	case rest == "compare":
		h.visualCompare(w, r, projectID, email)
	case strings.HasPrefix(rest, "images/"):
		h.visualImage(w, r, projectID, strings.TrimPrefix(rest, "images/"))
	default:
		h.visualDelete(w, r, projectID, rest)
	}
}

func (h *ProjectHandler) visualOverview(w http.ResponseWriter, r *http.Request, projectID serviceproject.ID) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	overview, err := h.visual.Overview(r.Context(), projectID)
	if err != nil {
		sendVisualError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusOK, overview)
}

func (h *ProjectHandler) visualBaseline(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	email string,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body servicevisualdiff.BaselineInput
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	baseline, err := h.visual.SetBaseline(r.Context(), projectID, body, email)
	if err != nil {
		sendVisualError(w, err)
		return
	}
	// 202: the record exists and is real, but the pages are still being
	// photographed. The panel polls the overview until it stops running.
	httptransport.SendJSON(w, http.StatusAccepted, baseline)
}

func (h *ProjectHandler) visualCompare(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	email string,
) {
	if r.Method != http.MethodPost {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Label string `json:"label"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	comparison, err := h.visual.Compare(r.Context(), projectID, body.Label, email)
	if err != nil {
		sendVisualError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusAccepted, comparison)
}

func (h *ProjectHandler) visualImage(
	w http.ResponseWriter,
	r *http.Request,
	projectID serviceproject.ID,
	rawFile string,
) {
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	file, err := url.PathUnescape(rawFile)
	if err != nil {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid image name")
		return
	}
	data, err := h.visual.Image(r.Context(), projectID, file)
	if err != nil {
		sendVisualError(w, err)
		return
	}
	writeVisualImage(w, data, file)
}

func (h *ProjectHandler) visualDelete(
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
		httptransport.SendErr(w, http.StatusBadRequest, "invalid comparison id")
		return
	}
	if err := h.visual.Delete(r.Context(), projectID, servicevisualdiff.ID(rawID)); err != nil {
		sendVisualError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeVisualImage sends the PNG with caching that matches what it is: an
// immutable artifact of one moment, but a private one, so it is cached by the
// browser and by nothing in between.
func writeVisualImage(w http.ResponseWriter, data []byte, file string) {
	w.Header().Set("Content-Type", servicescreenshot.MIMEType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=3600, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Never let a stored image be interpreted as a document in the platform's
	// own origin.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Content-Disposition", `inline; filename="`+file+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func sendVisualError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicevisualdiff.ErrInvalidPort),
		errors.Is(err, servicevisualdiff.ErrInvalidPath),
		errors.Is(err, servicevisualdiff.ErrInvalidSize),
		errors.Is(err, servicevisualdiff.ErrNoPaths),
		errors.Is(err, servicevisualdiff.ErrTooManyPaths),
		errors.Is(err, serviceproject.ErrInvalidID):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicevisualdiff.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	// Not-running, no-baseline and busy are all "the platform is fine, the
	// project is not ready for this yet", which is a 409 the UI turns into a
	// sentence rather than an error toast.
	case errors.Is(err, servicevisualdiff.ErrNotRunning),
		errors.Is(err, servicevisualdiff.ErrNoBaseline),
		errors.Is(err, servicevisualdiff.ErrBusy),
		errors.Is(err, servicescreenshot.ErrToolingMissing):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, servicevisualdiff.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
