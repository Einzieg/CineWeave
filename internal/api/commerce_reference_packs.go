package api

import (
	"net/http"
	"time"

	"github.com/Einzieg/cineweave/internal/auth"
	"github.com/Einzieg/cineweave/internal/authz"
	commercepkg "github.com/Einzieg/cineweave/internal/commerce"
	"github.com/Einzieg/cineweave/internal/httpx"
)

func (s *Server) listCommerceProductReferencePacks(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	items, err := s.commerceCatalog.ListProductReferencePacks(
		r.Context(), s.db, project.OrganizationID, project.ID, r.URL.Query().Get("filter[status]"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	for index := range items {
		s.attachCommerceReferencePackPreviews(r, project.ID, &items[index])
	}
	httpx.WriteJSON(w, r, http.StatusOK, map[string]any{"items": items}, nil)
}

func (s *Server) getCommerceProductReferencePack(w http.ResponseWriter, r *http.Request, principal auth.Principal) {
	project, ok := s.requireProjectAccess(w, r, principal, r.PathValue("projectId"), authz.PermissionAssetRead)
	if !ok {
		return
	}
	item, err := s.commerceCatalog.GetProductReferencePack(
		r.Context(), s.db, project.OrganizationID, project.ID, r.PathValue("packId"),
	)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	s.attachCommerceReferencePackPreviews(r, project.ID, &item)
	httpx.WriteJSON(w, r, http.StatusOK, item, nil)
}

func (s *Server) attachCommerceReferencePackPreviews(r *http.Request, projectID string, pack *commercepkg.ProductReferencePack) {
	if s.storage == nil || pack == nil {
		return
	}
	for index := range pack.Items {
		var storageKey string
		if err := s.db.QueryRow(r.Context(), `
			SELECT storage_key FROM artifacts WHERE id = $1 AND project_id = $2
		`, pack.Items[index].ArtifactID, projectID).Scan(&storageKey); err != nil {
			continue
		}
		if preview, err := s.storage.PresignGetObject(r.Context(), storageKey, 15*time.Minute); err == nil {
			pack.Items[index].PreviewURL = preview.URL
		}
	}
}
