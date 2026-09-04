package httphandlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// deployResponse is what the settings panel and the chat header both render.
type deployResponse struct {
	Targets []serviceglobalsecrets.DeployTarget `json:"targets"`
}

// handleDeploy serves /api/projects/{id}/deploy: which live servers this
// project's agents may reach, and the switch that changes it.
//
//	GET   lists every server with this project's current access
//	PUT   {"key": "...", "enabled": bool} turns one on or off
//
// The PUT does not return until the container has been converged, so an
// operator who switched access off can rely on the key being gone when the
// response lands rather than at some later moment.
func (h *ProjectHandler) handleDeploy(
	w http.ResponseWriter,
	r *http.Request,
	id serviceproject.ID,
	email string,
) {
	if h.deploy == nil {
		httptransport.SendErr(w, http.StatusServiceUnavailable,
			"the secrets vault is unavailable, so deploy access cannot be managed")
		return
	}

	switch r.Method {
	case http.MethodGet:
		targets, err := h.deploy.DeployTargets(r.Context(), string(id))
		if err != nil {
			sendDeployError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, deployResponse{Targets: targets})

	case http.MethodPut:
		var body struct {
			Key     string `json:"key"`
			Enabled bool   `json:"enabled"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
			httptransport.SendErr(w, http.StatusBadRequest, "invalid json")
			return
		}
		target, err := h.deploy.SetDeployAccess(r.Context(), string(id), body.Key, body.Enabled, email)
		if err != nil {
			sendDeployError(w, err)
			return
		}
		httptransport.SendJSON(w, http.StatusOK, target)

	default:
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func sendDeployError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, serviceglobalsecrets.ErrNotFound):
		httptransport.SendErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, serviceglobalsecrets.ErrNotSSHSecret):
		httptransport.SendErr(w, http.StatusBadRequest, err.Error())
	// An "all projects" grant is wider than this switch can express. That is
	// a conflict to explain, not a bad request to reject: the operator has to
	// narrow it in the vault first.
	case errors.Is(err, serviceglobalsecrets.ErrInvalidScope):
		httptransport.SendErr(w, http.StatusConflict,
			"this server is granted to all projects; narrow its scope in Settings → Secrets "+
				"before switching it per project")
	case errors.Is(err, serviceglobalsecrets.ErrUnavailable):
		httptransport.SendErr(w, http.StatusServiceUnavailable, err.Error())
	default:
		httptransport.SendErr(w, http.StatusInternalServerError, err.Error())
	}
}
