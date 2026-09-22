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

	ErrWeComLoginDisabled = kratosErrors.BadRequest("WECOM_LOGIN_DISABLED", "wecom login is not configured")
	ErrInvalidWeComUser   = kratosErrors.Forbidden("INVALID_WECOM_USER", "not a corp member")
	ErrInvalidState       = kratosErrors.BadRequest("INVALID_STATE", "invalid or expired state")
	ErrInvalidCode        = kratosErrors.BadRequest("INVALID_CODE", "wecom code exchange failed")
	ErrInvalidTicket      = kratosErrors.BadRequest("INVALID_TICKET", "invalid or expired ticket")
)
