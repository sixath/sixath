package service

import (
	"context"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/growth"
)

func newCronSkillRefRewriter(uc *biz.CronRefRewriteUsecase, helper *log.Helper) growth.CronSkillRefRewriter {
	if uc == nil {
		return nil
	}
	return func(ctx context.Context, workspaceKey string, renames map[string]string) error {
		n, err := uc.RewriteForWorkspace(ctx, workspaceKey, renames)
		if err != nil {
			return err
		}
		if n > 0 && helper != nil {
			helper.Infof("cron skill refs rewritten workspace=%s tasks=%d", workspaceKey, n)
		}
		return nil
	}
}
