package biz

import kratosErrors "github.com/go-kratos/kratos/v2/errors"

var (
	ErrForbiddenPerm     = kratosErrors.Forbidden("FORBIDDEN_PERM", "insufficient permission")
	ErrPublicNotEnabled  = kratosErrors.BadRequest("PUBLIC_NOT_ENABLED", "public visibility is not enabled")
	ErrInvalidHomeOrg    = kratosErrors.BadRequest("INVALID_HOME_ORG", "invalid home organization")
	ErrInvalidOrgContext = kratosErrors.BadRequest("INVALID_ORG_CONTEXT", "caller is not a member of the requested organization")
	ErrUnauthorized      = kratosErrors.Unauthorized("UNAUTHORIZED", "invalid email or password")
	ErrConflict          = kratosErrors.Conflict("CONFLICT", "email already registered")
	ErrBadRequest        = kratosErrors.BadRequest("INVALID_INVITE", "invalid or expired invite")
)
