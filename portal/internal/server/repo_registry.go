package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

const maxJSONBodyBytes = 1 << 20

// RepoRegistryHandlers serves /api/v1/repos, /api/v1/repo-groups and agent repo bindings.
type RepoRegistryHandlers struct {
	uc      *biz.RepoRegistryUsecase
	agentUC *biz.AgentUsecase
}

func NewRepoRegistryHandlers(uc *biz.RepoRegistryUsecase, agentUC *biz.AgentUsecase) *RepoRegistryHandlers {
	return &RepoRegistryHandlers{uc: uc, agentUC: agentUC}
}

func repoRegistryErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, biz.ErrInvalidRepo), errors.Is(err, biz.ErrInvalidRepoBinding), errors.Is(err, biz.ErrInvalidRepoGroup):
		return kratosErrors.BadRequest("INVALID_ARGUMENT", err.Error())
	case errors.Is(err, biz.ErrRepoNotFound):
		return kratosErrors.NotFound("NOT_FOUND", err.Error())
	case errors.Is(err, biz.ErrRepoScanRunning):
		return kratosErrors.Conflict("REPO_SCAN_RUNNING", err.Error())
	case errors.Is(err, biz.ErrRepoGroupInUse):
		return kratosErrors.Conflict("REPO_GROUP_IN_USE", err.Error())
	default:
		// ACL / agent-not-found errors are already kratos errors; keep their status codes.
		return err
	}
}

func decodeJSONBody(ctx kratoshttp.Context, dst any) error {
	body, err := io.ReadAll(io.LimitReader(ctx.Request().Body, maxJSONBodyBytes+1))
	if err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "read body failed")
	}
	if len(body) > maxJSONBodyBytes {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "body too large")
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
	}
	return nil
}

// requestActor returns the authenticated caller id; ctx must come from runWithMiddleware.
func requestActor(ctx context.Context) string {
	if id, ok := biz.CallerUserID(ctx); ok {
		return id
	}
	return "unknown"
}

func (h *RepoRegistryHandlers) serve(ctx kratoshttp.Context, fn func(context.Context) (any, error)) error {
	out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
		v, err := fn(c)
		return v, repoRegistryErr(err)
	})
	if err != nil {
		return err
	}
	return ctx.JSON(200, out)
}

// GET /api/v1/repos?status=&q=&group_id=&code_root=
func (h *RepoRegistryHandlers) ListRepos() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		q := ctx.Query()
		f := biz.RepoFilter{
			Status: strings.TrimSpace(q.Get("status")), Query: strings.TrimSpace(q.Get("q")),
			GroupID: strings.TrimSpace(q.Get("group_id")), CodeRoot: strings.TrimSpace(q.Get("code_root")),
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			items, err := h.uc.ListRepos(c, f)
			if err != nil {
				return nil, err
			}
			if items == nil {
				items = []*biz.Repository{}
			}
			return map[string]any{"items": items, "total": len(items)}, nil
		})
	}
}

// GET /api/v1/repos/{id}
func (h *RepoRegistryHandlers) GetRepo() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) { return h.uc.GetRepo(c, id) })
	}
}

// PATCH /api/v1/repos/{id}
func (h *RepoRegistryHandlers) PatchRepo() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var p biz.RepoMetaPatch
		if err := decodeJSONBody(ctx, &p); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) { return h.uc.PatchRepo(c, id, p) })
	}
}

// POST /api/v1/repos/scan
func (h *RepoRegistryHandlers) Scan() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		return h.serve(ctx, func(c context.Context) (any, error) { return h.uc.Scan(c) })
	}
}

// POST /api/v1/repos/migrate-legacy-links?apply=true (default: dry run)
func (h *RepoRegistryHandlers) MigrateLegacyLinks() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		apply := strings.EqualFold(strings.TrimSpace(ctx.Query().Get("apply")), "true")
		return h.serve(ctx, func(c context.Context) (any, error) {
			canEdit := func(c context.Context, agentID string) bool {
				_, err := h.agentUC.GetForEdit(c, agentID)
				return err == nil
			}
			items, err := h.uc.MigrateLegacyLinks(c, apply, canEdit)
			if err != nil {
				return nil, err
			}
			if items == nil {
				items = []*biz.LegacyLinkMigrationItem{}
			}
			return map[string]any{"apply": apply, "items": items}, nil
		})
	}
}

// GET /api/v1/repo-groups?kind=
func (h *RepoRegistryHandlers) ListGroups() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		kind := strings.TrimSpace(ctx.Query().Get("kind"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			items, err := h.uc.ListGroups(c, kind)
			if err != nil {
				return nil, err
			}
			if items == nil {
				items = []*biz.RepoGroupView{}
			}
			return map[string]any{"items": items}, nil
		})
	}
}

// POST /api/v1/repo-groups {"name":"...","repo_ids":[...]} — manual groups only.
func (h *RepoRegistryHandlers) CreateGroup() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		var req struct {
			Name    string   `json:"name"`
			RepoIDs []string `json:"repo_ids"`
		}
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			return h.uc.CreateManualGroup(c, req.Name, req.RepoIDs, requestActor(c))
		})
	}
}

// PUT /api/v1/repo-groups/{id}/members {"repo_ids":[...]}
func (h *RepoRegistryHandlers) SetGroupMembers() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		var req struct {
			RepoIDs []string `json:"repo_ids"`
		}
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			return map[string]any{"ok": true}, h.uc.SetManualGroupMembers(c, id, req.RepoIDs)
		})
	}
}

// DELETE /api/v1/repo-groups/{id}
func (h *RepoRegistryHandlers) DeleteGroup() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			return map[string]any{"ok": true}, h.uc.DeleteGroup(c, id)
		})
	}
}

// GET /api/v1/agents/{agent_id}/repo-bindings
func (h *RepoRegistryHandlers) GetBindings() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			if _, err := h.agentUC.GetForEdit(c, agentID); err != nil {
				return nil, err
			}
			return h.uc.GetBindings(c, agentID)
		})
	}
}

// PUT /api/v1/agents/{agent_id}/repo-bindings {"bindings":[...]}; "bindings":[] clears.
func (h *RepoRegistryHandlers) PutBindings() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		var req struct {
			Bindings *[]*biz.AgentRepoBinding `json:"bindings"`
		}
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		if req.Bindings == nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "bindings is required")
		}
		bindings := *req.Bindings
		return h.serve(ctx, func(c context.Context) (any, error) {
			if _, err := h.agentUC.GetForEdit(c, agentID); err != nil {
				return nil, err
			}
			return h.uc.ReplaceBindings(c, agentID, bindings, requestActor(c))
		})
	}
}

// POST /api/v1/agents/{agent_id}/repo-bindings/copy-from/{other_id}
// Requires edit on the target agent and view on the source agent.
func (h *RepoRegistryHandlers) CopyBindings() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		otherID := strings.TrimSpace(ctx.Vars().Get("other_id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			if _, err := h.agentUC.GetForEdit(c, agentID); err != nil {
				return nil, err
			}
			if _, err := h.agentUC.Get(c, otherID); err != nil {
				return nil, err
			}
			return h.uc.CopyBindings(c, otherID, agentID, requestActor(c))
		})
	}
}
