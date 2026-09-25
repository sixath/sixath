package data

import (
	"context"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.EvolutionProposalRepo = (*evolutionProposalRepo)(nil)

type evolutionProposalRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// EvolutionProposal is the GORM model for the evolution_proposals table.
type EvolutionProposal struct {
	ID                  string     `gorm:"column:id;primaryKey;size:36"`
	AgentID             string     `gorm:"column:agent_id;size:36;not null;index:idx_ep_agent"`
	SessionID           string     `gorm:"column:session_id;size:36;not null"`
	TurnIndex           int        `gorm:"column:turn_index;not null;default:0"`
	SignalType          string     `gorm:"column:signal_type;size:32;not null"`
	Confidence          float64    `gorm:"column:confidence;type:decimal(3,2);not null;default:0.00"`
	ProblemSummary      string     `gorm:"column:problem_summary;type:text;not null"`
	ProposedContent     string     `gorm:"column:proposed_content;type:text;not null"`
	TargetPath          string     `gorm:"column:target_path;size:512;not null;default:''"`
	TargetAction        string     `gorm:"column:target_action;size:32;not null;default:'create'"`
	Conflict            bool       `gorm:"column:conflict;not null;default:0"`
	ConflictDetail      *string    `gorm:"column:conflict_detail;type:text"`
	ConflictCheckFailed bool       `gorm:"column:conflict_check_failed;not null;default:0"`
	DedupSkipped        bool       `gorm:"column:dedup_skipped;not null;default:0"`
	Status              string     `gorm:"column:status;size:16;not null;default:'pending';index:idx_ep_status"`
	ReviewComment       *string    `gorm:"column:review_comment;type:text"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null"`
	ReviewedAt          *time.Time `gorm:"column:reviewed_at"`
	ReviewedBy          *string    `gorm:"column:reviewed_by;size:64"`
}

func (EvolutionProposal) TableName() string {
	return "evolution_proposals"
}

func NewEvolutionProposalRepo(data *Data, logger log.Logger) biz.EvolutionProposalRepo {
	if data == nil || data.db == nil {
		panic("NewEvolutionProposalRepo: Data.db is nil")
	}
	return &evolutionProposalRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *evolutionProposalRepo) Create(ctx context.Context, p *biz.EvolutionProposal) error {
	m := &EvolutionProposal{
		ID:                  uuid.New().String(),
		AgentID:             p.AgentID,
		SessionID:           p.SessionID,
		TurnIndex:           p.TurnIndex,
		SignalType:          p.SignalType,
		Confidence:          p.Confidence,
		ProblemSummary:      p.ProblemSummary,
		ProposedContent:     p.ProposedContent,
		TargetPath:          p.TargetPath,
		TargetAction:        p.TargetAction,
		Conflict:            p.Conflict,
		ConflictDetail:      p.ConflictDetail,
		ConflictCheckFailed: p.ConflictCheckFailed,
		DedupSkipped:        p.DedupSkipped,
		Status:              "pending",
		CreatedAt:           time.Now(),
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *evolutionProposalRepo) GetByID(ctx context.Context, id string) (*biz.EvolutionProposal, error) {
	var m EvolutionProposal
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return evolutionProposalModelToBiz(&m), nil
}

func (r *evolutionProposalRepo) List(ctx context.Context, page, pageSize int32, status string) ([]*biz.EvolutionProposal, int, error) {
	q := r.db.WithContext(ctx).Model(&EvolutionProposal{})
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var models []EvolutionProposal
	if err := q.Order("created_at DESC").Offset(int((page - 1) * pageSize)).Limit(int(pageSize)).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	items := make([]*biz.EvolutionProposal, len(models))
	for i := range models {
		items[i] = evolutionProposalModelToBiz(&models[i])
	}
	return items, int(total), nil
}

func (r *evolutionProposalRepo) Update(ctx context.Context, id string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&EvolutionProposal{}).Where("id = ?", id).Updates(updates).Error
}

func (r *evolutionProposalRepo) CountPending(ctx context.Context) (int, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&EvolutionProposal{}).Where("status = ?", "pending").Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *evolutionProposalRepo) ListExpired(ctx context.Context, before time.Time) ([]*biz.EvolutionProposal, error) {
	var models []EvolutionProposal
	if err := r.db.WithContext(ctx).
		Where("status = 'pending' AND created_at < ?", before).
		Find(&models).Error; err != nil {
		return nil, err
	}
	items := make([]*biz.EvolutionProposal, len(models))
	for i := range models {
		items[i] = evolutionProposalModelToBiz(&models[i])
	}
	return items, nil
}

func evolutionProposalModelToBiz(m *EvolutionProposal) *biz.EvolutionProposal {
	return &biz.EvolutionProposal{
		ID:                  m.ID,
		AgentID:             m.AgentID,
		SessionID:           m.SessionID,
		TurnIndex:           m.TurnIndex,
		SignalType:          m.SignalType,
		Confidence:          m.Confidence,
		ProblemSummary:      m.ProblemSummary,
		ProposedContent:     m.ProposedContent,
		TargetPath:          m.TargetPath,
		TargetAction:        m.TargetAction,
		Conflict:            m.Conflict,
		ConflictDetail:      m.ConflictDetail,
		ConflictCheckFailed: m.ConflictCheckFailed,
		DedupSkipped:        m.DedupSkipped,
		Status:              m.Status,
		ReviewComment:       m.ReviewComment,
		CreatedAt:           m.CreatedAt,
		ReviewedAt:          m.ReviewedAt,
		ReviewedBy:          m.ReviewedBy,
	}
}