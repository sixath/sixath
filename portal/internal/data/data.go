package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"backend/internal/conf"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// MySQL pool defaults: recycle before typical server wait_timeout so idle
// connections are not reused after the server already closed them.
const (
	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 5 * time.Minute
	defaultConnMaxIdleTime = 3 * time.Minute
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, ProvideDataRoot, ProvideCodeRoots, NewSessionUnitsBackendFromData, NewTurnTraceStoreFromData, NewToolRepo, NewMcpServerRepo, NewAgentRepo, NewIdentityRepo, NewInviteRepo, NewResourceRepo, NewChatSessionRepo, NewChatMessageRepo, NewChannelRepo, NewChannelRuntimeRepo, NewChannelPeerSessionRepo, NewCronTaskRepo, NewCronRunRepo, NewGrowthRepo, NewCuratorRepo)

// Data .
type Data struct {
	db *gorm.DB
}

// ProvideDataRoot exposes the configured per-agent workspace root to use cases.
func ProvideDataRoot(c *conf.Data) string {
	if c == nil {
		return ""
	}
	return c.GetDataRoot()
}

// ProvideCodeRoots returns a trimmed copy of configured code roots for browse/link APIs.
func ProvideCodeRoots(c *conf.Data) []string {
	if c == nil {
		return nil
	}
	roots := c.GetCodeRoots()
	if len(roots) == 0 {
		return nil
	}
	out := make([]string, 0, len(roots))
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// NewData initializes MySQL, migrates the schema, and bootstraps ACL anchors.
func NewData(c *conf.Data, auth *conf.Auth, logger log.Logger) (*Data, func(), error) {
	dbConf := c.GetDatabase()
	if dbConf == nil || dbConf.GetSource() == "" {
		return nil, nil, errors.New("database config required: set data.database.source in config")
	}

	db, err := gorm.Open(mysql.Open(dbConf.GetSource()), &gorm.Config{})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, err
	}
	configureSQLPool(sqlDB)

	// AutoMigrate 创建/更新表结构（按架构设计 docs/architecture_design.md）
	if err := db.AutoMigrate(
		&model.Tool{}, &model.Agent{}, &model.AgentTool{}, &model.ChatSession{}, &model.ChatMessage{},
		&model.Channel{}, &model.ChannelRuntimeStatus{}, &model.ChannelPeerSession{}, &model.CronTask{}, &model.CronRun{},
		&model.ChatGrowthState{}, &model.GrowthWorkspaceLease{},
		&model.GrowthCuratorState{},
		&model.User{}, &model.Org{}, &model.OrgMember{}, &model.UserToken{},
		&model.OrgInvite{}, &model.EmailVerifyToken{},
		&model.Resource{}, &model.ResourceGrant{},
		&MemoryUnit{},
		&model.TurnTraceRow{},
		&model.AgentAssetBinding{},
		&model.McpServer{}, &model.AgentMcpServer{},
		&model.PortalSetting{},
	); err != nil {
		return nil, nil, err
	}

	if err := BootstrapACL(context.Background(), db, auth, c.GetDataRoot()); err != nil {
		return nil, nil, err
	}
	log.NewHelper(logger).Info("ACL bootstrap completed")

	cleanup := func() {
		log.Info("closing the data resources")
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	}

	return &Data{db: db}, cleanup, nil
}

// DB exposes the underlying gorm DB (for BindingStore wiring).
func (d *Data) DB() *gorm.DB {
	if d == nil {
		return nil
	}
	return d.db
}

// Ping checks the underlying SQL connection (used by /readyz).
func (d *Data) Ping(ctx context.Context) error {
	if d == nil || d.db == nil {
		return errors.New("database not initialized")
	}
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

func configureSQLPool(sqlDB *sql.DB) {
	if sqlDB == nil {
		return
	}
	sqlDB.SetMaxOpenConns(defaultMaxOpenConns)
	sqlDB.SetMaxIdleConns(defaultMaxIdleConns)
	sqlDB.SetConnMaxLifetime(defaultConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(defaultConnMaxIdleTime)
}
