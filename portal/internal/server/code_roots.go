package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	fwws "github.com/sixath/framework/workspace"
)

// CodeRootsListHandler serves GET /api/v1/code-roots.
// Returns configured roots that exist as directories: {"roots":[...]}.
func CodeRootsListHandler(codeRoots []string) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return map[string]any{"roots": existingCodeRoots(codeRoots)}, nil
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// CodeRootsBrowseHandler serves GET /api/v1/code-roots/browse?root=&path=.
// Lists directories under root/path. Invalid root/path → 400; missing path → 404.
func CodeRootsBrowseHandler(codeRoots []string) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		rootParam := strings.TrimSpace(ctx.Query().Get("root"))
		pathParam := strings.TrimSpace(ctx.Query().Get("path"))

		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return browseCodeRoots(codeRoots, rootParam, pathParam)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func existingCodeRoots(codeRoots []string) []string {
	normalized := chat.NormalizeCodeRoots(codeRoots)
	if len(normalized) == 0 {
		return []string{}
	}
	existing := make([]string, 0, len(normalized))
	for _, r := range normalized {
		fi, err := os.Stat(r)
		if err != nil || !fi.IsDir() {
			continue
		}
		existing = append(existing, r)
	}
	return existing
}

func browseCodeRoots(codeRoots []string, rootParam, pathParam string) (any, error) {
	if rootParam == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "root required")
	}
	if strings.ContainsRune(pathParam, 0) {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "path invalid")
	}
	root, err := matchConfiguredRoot(codeRoots, rootParam)
	if err != nil {
		return nil, err
	}
	entries, err := chat.ListCodeDirs(root, pathParam)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, kratosErrors.NotFound("NOT_FOUND", "path not found")
		}
		// Escape / absolute / invalid relative → 400
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", err.Error())
	}
	if entries == nil {
		entries = []chat.CodeDirEntry{}
	}
	return map[string]any{
		"root":    root,
		"path":    pathParam,
		"entries": entries,
	}, nil
}

func matchConfiguredRoot(codeRoots []string, rootParam string) (string, error) {
	normalized := chat.NormalizeCodeRoots(codeRoots)
	want := chat.NormalizeCodeRoots([]string{rootParam})
	if len(want) == 0 {
		return "", kratosErrors.BadRequest("INVALID_ARGUMENT", "root required")
	}
	reqRoot := want[0]
	for _, r := range normalized {
		if r == reqRoot {
			return r, nil
		}
	}
	return "", kratosErrors.BadRequest("INVALID_ARGUMENT", "root not in code_roots")
}

// AgentWorkspaceLinkGetHandler serves GET /api/v1/agents/{agent_id}/workspace-link.
// Returns whether workspace/code exists and its resolved target (for edit-form hydrate).
func AgentWorkspaceLinkGetHandler(agentUC *biz.AgentUsecase) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			agent, err := agentUC.GetForEdit(c, agentID)
			if err != nil {
				return nil, err
			}
			return workspaceCodeLinkStatus(agent.Workspace)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// AgentWorkspaceLinkHandler serves POST /api/v1/agents/{agent_id}/workspace-link.
// Body: {"target":"/abs/path/under/code/root"} — creates workspace/code → target symlink.
func AgentWorkspaceLinkHandler(agentUC *biz.AgentUsecase, codeRoots []string) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		if agentID == "" {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "agent_id required")
		}
		var req struct {
			Target string `json:"target"`
		}
		body, _ := io.ReadAll(ctx.Request().Body)
		if err := json.Unmarshal(body, &req); err != nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid json body")
		}

		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			agent, err := agentUC.GetForEdit(c, agentID)
			if err != nil {
				return nil, err
			}
			return linkWorkspaceCode(agent.Workspace, req.Target, codeRoots)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func workspaceCodeLinkStatus(workspace string) (map[string]any, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return map[string]any{"exists": false}, nil
	}
	link := filepath.Join(workspace, chat.WorkspaceCodeLink)
	fi, err := os.Lstat(link)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{"exists": false, "link": link}, nil
		}
		return nil, kratosErrors.InternalServer("INTERNAL", err.Error())
	}
	target, resolveErr := resolveLinkTarget(link)
	out := map[string]any{
		"exists": true,
		"link":   link,
		"is_dir": fi.IsDir() || fi.Mode()&os.ModeSymlink != 0,
	}
	if resolveErr == nil && target != "" {
		out["target"] = target
	}
	return out, nil
}

// linkWorkspaceCode creates {workspace}/code → absTarget under code_roots.
func linkWorkspaceCode(wsRoot, target string, codeRoots []string) (any, error) {
	link, absTarget, err := fwws.LinkCode(wsRoot, target, codeRoots)
	if err != nil {
		switch {
		case errors.Is(err, fwws.ErrEmptyWorkspace):
			return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "agent workspace empty")
		case errors.Is(err, fwws.ErrEmptyTarget):
			return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "target required")
		case errors.Is(err, fwws.ErrTargetNotAllowed):
			return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "target not under code_roots")
		case errors.Is(err, fwws.ErrLinkConflict):
			return nil, kratosErrors.Conflict("WORKSPACE_LINK_CONFLICT", "workspace/code already exists with a different target")
		default:
			return nil, kratosErrors.InternalServer("INTERNAL", err.Error())
		}
	}
	return map[string]any{"link": link, "target": absTarget}, nil
}

func resolveLinkTarget(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", os.ErrNotExist
	}
	if eval, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(eval), nil
	}
	// Windows directory junctions often fail EvalSymlinks; Readlink still works.
	if rl, err := os.Readlink(path); err == nil {
		rl = strings.TrimSpace(rl)
		if abs, absErr := filepath.Abs(rl); absErr == nil {
			return filepath.Clean(abs), nil
		}
		return filepath.Clean(rl), nil
	}
	// Plain directory / file path (wantTarget side).
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}
