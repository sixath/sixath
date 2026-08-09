package biz

import "context"

// Mailer sends optional transactional auth emails (verify-email, etc.).
type Mailer interface {
	SendVerifyEmail(ctx context.Context, email, verifyToken string) error
}

// NoopMailer discards outbound mail; used when SMTP is not configured.
type NoopMailer struct{}

func (NoopMailer) SendVerifyEmail(context.Context, string, string) error {
	return nil
}
