package data

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.RepoRegistryRepo = (*repoRegistryRepo)(nil)

// dirGroupNamespace derives stable dir-group ids from (code_root, rel_prefix).
var dirGroupNamespace = uuid.MustParse("6f1f0f2e-8a7c-4c1e-9b7d-3c2a1e5d4f60")

type repoRegistryRepo struct {
	db  *gorm.DB
	log *log.Helper
}

func NewRepoRegistryRepo(data *Data, logger log.Logger) biz.RepoRegistryRepo {
	if data == nil || data.db == nil {
		panic("NewRepoRegistryRepo: Data.db is nil")
	}
	return &repoRegistryRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *repoRegistryRepo) UpsertScannedRepository(ctx context.Context, in *biz.Repository) (*biz.Repository, error) {
	var out *biz.Repository
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var m model.Repository
		err := tx.Where("code_root = ? AND rel_path = ?", in.CodeRoot, in.RelPath).First(&m).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			m = model.Repository{
				ID: uuid.NewString(), CodeRoot: in.CodeRoot, RelPath: in.RelPath, Name: in.Name,
				GitRemote: in.GitRemote, GitBranch: in.GitBranch, HeadCommit: in.HeadCommit,
				SyncMode: biz.RepoSyncRegistryOnly, Status: biz.RepoStatusActive,
				HandbookStatus: biz.HandbookStatusNone, LastScannedAt: in.LastScannedAt,
			}
			if err := tx.Create(&m).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			updates := map[string]any{
				"git_remote": in.GitRemote, "git_branch": in.GitBranch,
				"head_commit": in.HeadCommit, "last_scanned_at": in.LastScannedAt,
			}
			if m.Status != biz.RepoStatusArchived {
				updates["status"] = biz.RepoStatusActive
			}
			if err := tx.Model(&m).Updates(updates).Error; err != nil {
				return err
			}
			if err := tx.Where("id = ?", m.ID).First(&m).Error; err != nil {
				return err
			}
		}
		out = repositoryToBiz(&m)
		return nil
	})
	return out, err
}

func (r *repoRegistryRepo) ListRepositories(ctx context.Context, f biz.RepoFilter) ([]*biz.Repository, error) {
	q := r.db.WithContext(ctx).Model(&model.Repository{})
	if f.CodeRoot != "" {
		q = q.Where("code_root = ?", f.CodeRoot)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if s := strings.TrimSpace(f.Query); s != "" {
		like := "%" + s + "%"
		q = q.Where("(rel_path LIKE ? OR name LIKE ?)", like, like)
	}
	if f.GroupID != "" {
		sub := r.db.Model(&model.RepoGroupMember{}).Select("repo_id").
			Where("group_id = ? AND state = ?", f.GroupID, biz.RepoMemberActive)
		q = q.Where("id IN (?)", sub)
	}
	var ms []model.Repository
	if err := q.Order("code_root, rel_path").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.Repository, 0, len(ms))
	for i := range ms {
		out = append(out, repositoryToBiz(&ms[i]))
	}
	return out, nil
}

func (r *repoRegistryRepo) GetRepositoriesByIDs(ctx context.Context, ids []string) (map[string]*biz.Repository, error) {
	out := map[string]*biz.Repository{}
	if len(ids) == 0 {
		return out, nil
	}
	var ms []model.Repository
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&ms).Error; err != nil {
		return nil, err
	}
	for i := range ms {
		out[ms[i].ID] = repositoryToBiz(&ms[i])
	}
	return out, nil
}

func (r *repoRegistryRepo) SetRepositoryStatus(ctx context.Context, id, status string) error {
	res := r.db.WithContext(ctx).Model(&model.Repository{}).Where("id = ?", id).Update("status", status)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return biz.ErrRepoNotFound
	}
	return nil
}

func (r *repoRegistryRepo) UpdateRepositoryMeta(ctx context.Context, id string, p biz.RepoMetaPatch) (*biz.Repository, error) {
	updates := map[string]any{}
	if p.Name != nil {
		updates["name"] = strings.TrimSpace(*p.Name)
	}
	if p.Description != nil {
		updates["description"] = *p.Description
	}
	if p.Tags != nil {
		updates["tags"] = model.JSONStrings(*p.Tags)
	}
	if p.OwnerID != nil {
		updates["owner_id"] = *p.OwnerID
	}
	if p.Status != nil {
		updates["status"] = *p.Status
	}
	db := r.db.WithContext(ctx)
	if len(updates) > 0 {
		if err := db.Model(&model.Repository{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	var m model.Repository
	if err := db.Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, biz.ErrRepoNotFound
		}
		return nil, err
	}
	return repositoryToBiz(&m), nil
}

func (r *repoRegistryRepo) UpsertDirGroup(ctx context.Context, codeRoot, relPrefix string) (*biz.RepoGroup, error) {
	id := uuid.NewSHA1(dirGroupNamespace, []byte(codeRoot+"\x00"+relPrefix)).String()
	m := model.RepoGroup{
		ID: id, Name: relPrefix, Kind: biz.RepoGroupDir, AutoApplyNew: true,
		HandbookStatus: biz.HandbookStatusNone,
		Rule:           &model.RepoGroupRule{CodeRoot: codeRoot, RelPrefix: relPrefix},
	}
	if err := r.db.WithContext(ctx).Where("id = ?", id).FirstOrCreate(&m).Error; err != nil {
		return nil, err
	}
	return repoGroupToBiz(&m), nil
}

func (r *repoRegistryRepo) CreateGroup(ctx context.Context, g *biz.RepoGroup) (*biz.RepoGroup, error) {
	m := model.RepoGroup{
		ID: uuid.NewString(), Name: g.Name, Kind: g.Kind, AutoApplyNew: g.AutoApplyNew,
		HandbookStatus: biz.HandbookStatusNone, OwnerID: g.OwnerID, Rule: ruleToModel(g.Rule),
	}
	if err := r.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, err
	}
	return repoGroupToBiz(&m), nil
}

func (r *repoRegistryRepo) ListGroups(ctx context.Context, kind string) ([]*biz.RepoGroup, error) {
	q := r.db.WithContext(ctx).Model(&model.RepoGroup{})
	if kind != "" {
		q = q.Where("kind = ?", kind)
	}
	var ms []model.RepoGroup
	if err := q.Order("kind, name").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.RepoGroup, 0, len(ms))
	for i := range ms {
		out = append(out, repoGroupToBiz(&ms[i]))
	}
	return out, nil
}

func (r *repoRegistryRepo) GetGroupsByIDs(ctx context.Context, ids []string) (map[string]*biz.RepoGroup, error) {
	out := map[string]*biz.RepoGroup{}
	if len(ids) == 0 {
		return out, nil
	}
	var ms []model.RepoGroup
	if err := r.db.WithContext(ctx).Where("id IN ?", ids).Find(&ms).Error; err != nil {
		return nil, err
	}
	for i := range ms {
		out[ms[i].ID] = repoGroupToBiz(&ms[i])
	}
	return out, nil
}

func (r *repoRegistryRepo) DeleteGroup(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", id).Delete(&model.RepoGroupMember{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target_kind = ? AND target_id = ?", biz.RepoTargetGroup, id).Delete(&model.AgentRepoBinding{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&model.RepoGroup{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return biz.ErrRepoNotFound
		}
		return nil
	})
}

func (r *repoRegistryRepo) ReplaceGroupMembers(ctx context.Context, groupID, source string, repoIDs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ? AND source = ?", groupID, source).Delete(&model.RepoGroupMember{}).Error; err != nil {
			return err
		}
		if len(repoIDs) == 0 {
			return nil
		}
		now := time.Now()
		rows := make([]model.RepoGroupMember, 0, len(repoIDs))
		for _, id := range dedupeStrings(repoIDs) {
			rows = append(rows, model.RepoGroupMember{GroupID: groupID, RepoID: id, Source: source, State: biz.RepoMemberActive, CreatedAt: now})
		}
		return tx.Create(&rows).Error
	})
}

func (r *repoRegistryRepo) ListActiveGroupMembers(ctx context.Context, groupIDs []string) (map[string][]string, error) {
	out := map[string][]string{}
	if len(groupIDs) == 0 {
		return out, nil
	}
	var ms []model.RepoGroupMember
	if err := r.db.WithContext(ctx).Where("group_id IN ? AND state = ?", groupIDs, biz.RepoMemberActive).
		Order("group_id, repo_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	for _, m := range ms {
		out[m.GroupID] = append(out[m.GroupID], m.RepoID)
	}
	return out, nil
}

func (r *repoRegistryRepo) ListAgentBindings(ctx context.Context, agentID string) ([]*biz.AgentRepoBinding, error) {
	var ms []model.AgentRepoBinding
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).
		Order("priority, target_kind, target_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.AgentRepoBinding, 0, len(ms))
	for _, m := range ms {
		out = append(out, &biz.AgentRepoBinding{
			AgentID: m.AgentID, TargetKind: m.TargetKind, TargetID: m.TargetID, Mode: m.Mode,
			SubPaths: []string(m.SubPaths), Priority: m.Priority, CreatedBy: m.CreatedBy,
		})
	}
	return out, nil
}

func (r *repoRegistryRepo) ReplaceAgentBindings(ctx context.Context, agentID string, bs []*biz.AgentRepoBinding) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentRepoBinding{}).Error; err != nil {
			return err
		}
		if len(bs) == 0 {
			return nil
		}
		rows := make([]model.AgentRepoBinding, 0, len(bs))
		for _, b := range bs {
			rows = append(rows, model.AgentRepoBinding{
				AgentID: agentID, TargetKind: b.TargetKind, TargetID: b.TargetID, Mode: b.Mode,
				SubPaths: model.JSONStrings(b.SubPaths), Priority: b.Priority, CreatedBy: b.CreatedBy,
			})
		}
		return tx.Create(&rows).Error
	})
}

func (r *repoRegistryRepo) ListAgentIDsWithBindings(ctx context.Context) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentRepoBinding{}).Distinct().Order("agent_id").Pluck("agent_id", &ids).Error
	return ids, err
}

func (r *repoRegistryRepo) ListAgentIDsBoundToGroup(ctx context.Context, groupID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentRepoBinding{}).
		Where("target_kind = ? AND target_id = ?", biz.RepoTargetGroup, groupID).
		Distinct().Order("agent_id").Pluck("agent_id", &ids).Error
	return ids, err
}

func (r *repoRegistryRepo) ReplaceEffectiveRepos(ctx context.Context, agentID string, rows []*biz.AgentEffectiveRepo) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agentID).Delete(&model.AgentEffectiveRepo{}).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		ms := make([]model.AgentEffectiveRepo, 0, len(rows))
		for _, e := range rows {
			via := make(model.RepoBindingRefs, 0, len(e.Via))
			for _, v := range e.Via {
				via = append(via, model.RepoBindingRef{Kind: v.Kind, ID: v.ID})
			}
			ms = append(ms, model.AgentEffectiveRepo{
				AgentID: agentID, RepoID: e.RepoID, Via: via,
				SubPaths: model.JSONStrings(e.SubPaths), ComputedAt: e.ComputedAt,
			})
		}
		return tx.Create(&ms).Error
	})
}

func (r *repoRegistryRepo) ListEffectiveRepos(ctx context.Context, agentID string) ([]*biz.AgentEffectiveRepo, error) {
	var ms []model.AgentEffectiveRepo
	if err := r.db.WithContext(ctx).Where("agent_id = ?", agentID).Order("repo_id").Find(&ms).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.AgentEffectiveRepo, 0, len(ms))
	for _, m := range ms {
		via := make([]biz.BindingRef, 0, len(m.Via))
		for _, v := range m.Via {
			via = append(via, biz.BindingRef{Kind: v.Kind, ID: v.ID})
		}
		out = append(out, &biz.AgentEffectiveRepo{
			AgentID: m.AgentID, RepoID: m.RepoID, Via: via,
			SubPaths: []string(m.SubPaths), ComputedAt: m.ComputedAt,
		})
	}
	return out, nil
}

func (r *repoRegistryRepo) ListAgentIDsByEffectiveRepo(ctx context.Context, repoID string) ([]string, error) {
	var ids []string
	err := r.db.WithContext(ctx).Model(&model.AgentEffectiveRepo{}).Where("repo_id = ?", repoID).
		Order("agent_id").Pluck("agent_id", &ids).Error
	return ids, err
}

func repositoryToBiz(m *model.Repository) *biz.Repository {
	return &biz.Repository{
		ID: m.ID, CodeRoot: m.CodeRoot, RelPath: m.RelPath, Name: m.Name, Description: m.Description,
		Tags: []string(m.Tags), GitRemote: m.GitRemote, GitBranch: m.GitBranch, HeadCommit: m.HeadCommit,
		SyncMode: m.SyncMode, Status: m.Status, HandbookStatus: m.HandbookStatus, OwnerID: m.OwnerID,
		LastScannedAt: m.LastScannedAt, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func repoGroupToBiz(m *model.RepoGroup) *biz.RepoGroup {
	g := &biz.RepoGroup{
		ID: m.ID, Name: m.Name, Kind: m.Kind, AutoApplyNew: m.AutoApplyNew,
		OwnerID: m.OwnerID, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
	if m.Rule != nil {
		g.Rule = &biz.RepoGroupRule{CodeRoot: m.Rule.CodeRoot, RelPrefix: m.Rule.RelPrefix, AllOf: m.Rule.AllOf, AnyOf: m.Rule.AnyOf}
	}
	return g
}

func ruleToModel(r *biz.RepoGroupRule) *model.RepoGroupRule {
	if r == nil {
		return nil
	}
	return &model.RepoGroupRule{CodeRoot: r.CodeRoot, RelPrefix: r.RelPrefix, AllOf: r.AllOf, AnyOf: r.AnyOf}
}

func dedupeStrings(in []string) []string {
	set := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := set[s]; ok || s == "" {
			continue
		}
		set[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
