package httphandlers

import (
	"net/http"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	httptransport "github.com/futrx-com/remote.futrx.com/internal/transport/http"
)

// trashedProjectResponse is a project in the trash. It is the normal project
// payload plus the two numbers the Trash page needs: when it was deleted and
// when the janitor will make that permanent.
type trashedProjectResponse struct {
	serviceproject.Meta
	// ExpiresAt is the unix-ms instant the retention sweep purges this
	// project. Zero when retention is disabled, which means "kept until an
	// admin purges it".
	ExpiresAt int64 `json:"expiresAt,omitempty"`
}

// HandleTrash serves GET /api/projects/trash. Admins see every trashed
// project; members see the ones they belonged to, so the person who deleted a
// project by accident can undo it without an admin.
func (h *ProjectHandler) HandleTrash(w http.ResponseWriter, r *http.Request) {
	email, isAdmin, err := h.caller(r)
	if err != nil {
		httptransport.SendErr(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if r.Method != http.MethodGet {
		httptransport.SendErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	metas, err := h.projects.ListTrashed(r.Context(), email, isAdmin)
	if err != nil {
		sendProjectError(w, err)
		return
	}
	out := make([]trashedProjectResponse, 0, len(metas))
	for _, meta := range metas {
		out = append(out, trashedProject(meta, h.trashRetention))
	}
	httptransport.SendJSON(w, http.StatusOK, out)
}

func trashedProject(meta serviceproject.Meta, retention time.Duration) trashedProjectResponse {
	return trashedProjectResponse{Meta: meta, ExpiresAt: meta.TrashExpiresAt(retention)}
}
