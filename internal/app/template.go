package app

import (
	"context"

	"git-task/internal/storage"
)

func (p *Plans) InitTemplate(ctx context.Context, apply bool) (storage.TemplateResult, bool, error) {
	exists, err := p.git.HasCommit(ctx)
	if err != nil {
		return storage.TemplateResult{}, false, err
	}
	result, err := p.store.InitTemplate(ctx, apply)
	return result, !exists, err
}
