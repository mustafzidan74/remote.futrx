package httphandlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// snapshotsResponse is what the Snapshots tab polls: the records plus the
// background jobs, so one request answers both "what exists" and "is
// something running right now".
type snapshotsResponse struct {
	Snapshots []servicesnapshot.Snapshot `json:"snapshots"`
	Jobs      []servicesnapshot.Job      `json:"jobs"`
}

// handleSnapshots serves /api/projects/{id}/snapshots[/{sid}[/restore]].
// Project membership was already established by HandleResource.
//
//	GET    .../snapshots                 list records and running jobs
//	POST   .../snapshots                 start a snapshot (202)
//	POST   .../snapshots/{sid}/restore   replace the project files (202)
//	DELETE .../snapshots/{sid}           drop one record and its archive
func (h *ProjectHandler) handleSnapshots(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	parts []string,
	email string,
	isAdmin bool,
) {
	if h.snapshots == nil || !h.snapshots.Available() {
		httptransport.SendErr(w, http.StatusServiceUnavailable, servicesnapshot.ErrUnavailable.Error())
		return
	}

	rest := ""
	if len(parts) >= 3 {
		rest = strings.Trim(parts[2], "/")
	}
	if rest == "" {
		switch r.Method {
		case http.MethodGet:
			h.listSnapshots(w, r, id)
		case http.MethodPost:
			h.createSnapshot(w, r, id, email)
		default:
			httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		}
		return
	}

	segments := strings.SplitN(rest, "/", 2)
	rawID, err := url.PathUnescape(segments[0])
	if err != nil || rawID == "" {
		httptransport.SendErr(w, http.StatusBadRequest, "missing snapshot id")
		return
	}
	snapshotID := servicesnapshot.ID(rawID)

	action := ""
	if len(segments) == 2 {
		action = strings.Trim(segments[1], "/")
	}
	switch {
	case action == "restore" && r.Method == http.MethodPost:
		h.restoreSnapshot(w, r, id, snapshotID, email, isAdmin)
	case action == "" && r.Method == http.MethodDelete:
		if err := h.snapshots.Delete(r.Context(), id, snapshotID); err != nil {
			sendSnapshotError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case action == "":
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	default:
		httptransport.SendErr(w, http.StatusNotFound, "unknown action")
	}
}

func (h *ProjectHandler) listSnapshots(w http.ResponseWriter, r *http.Request, id serviceproject.ID) {
	records, err := h.snapshots.List(r.Context(), id)
	if err != nil {
		sendSnapshotError(w, err)
		return
	}
	if records == nil {
		records = []servicesnapshot.Snapshot{}
	}
	jobs := h.snapshots.Jobs(id)
	if jobs == nil {
		jobs = []servicesnapshot.Job{}
	}
	httptransport.SendJSON(w, http.StatusOK, snapshotsResponse{Snapshots: records, Jobs: jobs})
}

func (h *ProjectHandler) createSnapshot(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	email string,
) {
	body, ok := decodeSnapshotBody(w, r)
	if !ok {
		return
	}
	record, err := h.snapshots.Create(r.Context(), id, body, email)
	if err != nil {
		sendSnapshotError(w, err)
		return
	}
	// 202: the record exists, its archive does not yet. Poll the list.
	httptransport.SendJSON(w, http.StatusAccepted, record)
}

// restoreSnapshot replaces the project's files. An admin may do it outright;
// a member has to acknowledge that the current files are being replaced by
// sending {"confirm":true}.
func (h *ProjectHandler) restoreSnapshot(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	snapshotID servicesnapshot.ID,
	email string,
	isAdmin bool,
) {
	body, ok := decodeSnapshotBody(w, r)
	if !ok {
		return
	}
	job, err := h.snapshots.Restore(r.Context(), id, snapshotID, isAdmin || body.Confirm, email)
	if err != nil {
		sendSnapshotError(w, err)
		return
	}
	httptransport.SendJSON(w, http.StatusAccepted, job)
}

// decodeSnapshotBody accepts an absent body: "snapshot now" with no label and
// no options is the common case.
func decodeSnapshotBody(w http.ResponseWriter, r *http.Request) (servicesnapshot.CreateInput, bool) {
	var body servicesnapshot.CreateInput
	err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	if err != nil && !errors.Is(err, io.EOF) {
		httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
		return servicesnapshot.CreateInput{}, false
	}
	return body, true
}

func sendSnapshotError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, servicesnapshot.ErrLabelTooLong),
		errors.Is(err, servicesnapshot.ErrConfirmRequired):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, servicesnapshot.ErrBusy),
		errors.Is(err, servicesnapshot.ErrNotReady):
		httptransport.SendErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, servicesnapshot.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, servicesnapshot.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		sendProjectError(w, err)
	}
}
