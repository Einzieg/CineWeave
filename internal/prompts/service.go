package prompts

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) Resolve(ctx context.Context, req ResolveRequest) (ResolvedPrompt, error) {
	if s == nil || s.db == nil {
		return ResolvedPrompt{}, Error{Code: CodePromptTemplateNotFound, Message: "prompt registry database is not configured"}
	}
	req.OrganizationID = strings.TrimSpace(req.OrganizationID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.TemplateKey = strings.TrimSpace(req.TemplateKey)
	if req.OrganizationID == "" || req.TemplateKey == "" {
		return ResolvedPrompt{}, Error{Code: CodePromptTemplateNotFound, Message: "organizationId and templateKey are required"}
	}
	if req.ProjectID != "" {
		if resolved, ok, err := s.resolveOne(ctx, resolveProjectBindingSQL, req.OrganizationID, req.ProjectID, req.TemplateKey); err != nil {
			return ResolvedPrompt{}, err
		} else if ok {
			resolved.Source = "project_binding"
			return resolved, nil
		}
	}
	if resolved, ok, err := s.resolveOne(ctx, resolveOrganizationBindingSQL, req.OrganizationID, req.TemplateKey); err != nil {
		return ResolvedPrompt{}, err
	} else if ok {
		resolved.Source = "organization_binding"
		return resolved, nil
	}
	if resolved, ok, err := s.resolveOne(ctx, resolveOrganizationActiveSQL, req.OrganizationID, req.TemplateKey); err != nil {
		return ResolvedPrompt{}, err
	} else if ok {
		resolved.Source = "organization_active"
		return resolved, nil
	}
	if resolved, ok, err := s.resolveOne(ctx, resolveSystemActiveSQL, req.TemplateKey); err != nil {
		return ResolvedPrompt{}, err
	} else if ok {
		resolved.Source = "system_active"
		return resolved, nil
	}
	return ResolvedPrompt{}, Error{Code: CodePromptTemplateNotFound, Message: fmt.Sprintf("template %s has no active prompt version", req.TemplateKey)}
}

func (s *Service) resolveOne(ctx context.Context, query string, args ...any) (ResolvedPrompt, bool, error) {
	var resolved ResolvedPrompt
	err := s.db.QueryRow(ctx, query, args...).Scan(
		&resolved.TemplateID,
		&resolved.TemplateKey,
		&resolved.VersionID,
		&resolved.Version,
		&resolved.Content,
		&resolved.ContentHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ResolvedPrompt{}, false, nil
		}
		return ResolvedPrompt{}, false, err
	}
	return resolved, true, nil
}

const resolveProjectBindingSQL = `
	SELECT pt.id::text, pt.template_key, pv.id::text, COALESCE(pv.version, pv.version_no), pv.content, pv.content_hash
	FROM prompt_bindings b
	JOIN prompt_versions pv ON pv.id = b.prompt_version_id
	JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
	WHERE b.organization_id = $1
	  AND b.project_id = $2
	  AND b.template_key = $3
	  AND b.status = 'active'
	  AND pv.status = 'active'
	LIMIT 1
`

const resolveOrganizationBindingSQL = `
	SELECT pt.id::text, pt.template_key, pv.id::text, COALESCE(pv.version, pv.version_no), pv.content, pv.content_hash
	FROM prompt_bindings b
	JOIN prompt_versions pv ON pv.id = b.prompt_version_id
	JOIN prompt_templates pt ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
	WHERE b.organization_id = $1
	  AND b.project_id IS NULL
	  AND b.template_key = $2
	  AND b.status = 'active'
	  AND pv.status = 'active'
	LIMIT 1
`

const resolveOrganizationActiveSQL = `
	SELECT pt.id::text, pt.template_key, pv.id::text, COALESCE(pv.version, pv.version_no), pv.content, pv.content_hash
	FROM prompt_templates pt
	JOIN prompt_versions pv ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
	WHERE pt.organization_id = $1
	  AND pt.template_key = $2
	  AND pt.status = 'active'
	  AND pv.status = 'active'
	ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
	LIMIT 1
`

const resolveSystemActiveSQL = `
	SELECT pt.id::text, pt.template_key, pv.id::text, COALESCE(pv.version, pv.version_no), pv.content, pv.content_hash
	FROM prompt_templates pt
	JOIN prompt_versions pv ON pt.id = COALESCE(pv.template_id, pv.prompt_template_id)
	WHERE pt.organization_id IS NULL
	  AND pt.template_key = $1
	  AND pt.status = 'active'
	  AND pv.status = 'active'
	ORDER BY COALESCE(pv.activated_at, pv.created_at) DESC
	LIMIT 1
`
