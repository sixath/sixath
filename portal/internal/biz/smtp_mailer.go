package biz

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"

	"backend/internal/conf"
)

// SMTPMailer sends auth emails via SMTP.
type SMTPMailer struct {
	host           string
	port           int
	user           string
	password       string
	from           string
	publicBaseURL  string
}

// NewMailer returns NoopMailer when SMTP host is unset; otherwise an SMTP-backed mailer.
func NewMailer(auth *conf.Auth) Mailer {
	if auth == nil || strings.TrimSpace(auth.GetSmtpHost()) == "" {
		return NoopMailer{}
	}
	port := int(auth.GetSmtpPort())
	if port == 0 {
		port = 587
	}
	from := strings.TrimSpace(auth.GetSmtpFrom())
	if from == "" {
		from = strings.TrimSpace(auth.GetSmtpUser())
	}
	return &SMTPMailer{
		host:          strings.TrimSpace(auth.GetSmtpHost()),
		port:          port,
		user:          auth.GetSmtpUser(),
		password:      auth.GetSmtpPassword(),
		from:          from,
		publicBaseURL: strings.TrimSpace(auth.GetPublicBaseUrl()),
	}
}

func buildVerifyEmailLink(publicBaseURL, token string) string {
	if publicBaseURL != "" {
		return strings.TrimRight(publicBaseURL, "/") + "/verify-email?token=" + url.QueryEscape(token)
	}
	return "/verify-email?token=" + token
}

func (m *SMTPMailer) SendVerifyEmail(_ context.Context, email, verifyToken string) error {
	if m.from == "" {
		return fmt.Errorf("smtp from address required")
	}
	link := buildVerifyEmailLink(m.publicBaseURL, verifyToken)
	subject := "Verify your email"
	body := fmt.Sprintf("Please verify your email by visiting:\n\n%s\n", link)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		m.from, email, subject, body)
	return m.send(email, []byte(msg))
}

func (m *SMTPMailer) send(to string, msg []byte) error {
	addr := net.JoinHostPort(m.host, fmt.Sprintf("%d", m.port))
	var auth smtp.Auth
	if strings.TrimSpace(m.user) != "" {
		auth = smtp.PlainAuth("", m.user, m.password, m.host)
	}
	// Port 465 uses implicit TLS; 587 and others use STARTTLS when offered.
	if m.port == 465 {
		return sendMailTLS(addr, auth, m.from, []string{to}, msg)
	}
	return sendMailSTARTTLS(addr, m.host, auth, m.from, []string{to}, msg)
}

func sendMailSTARTTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func sendMailTLS(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return smtp.SendMail(addr, auth, from, to, msg)
}
