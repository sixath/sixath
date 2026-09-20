package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"backend/internal/chat"
	"backend/internal/service"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

const maxAttachmentMultipartMemory = 12 << 20 // 12MiB (covers 10MB image + overhead)

// Slack beyond MaxImageAttachmentBytes when reading the multipart file part.
const maxAttachmentFileReadSlack = 256 << 10 // 256KiB

// UploadAttachmentHandler serves POST /api/v1/sessions/{session_id}/attachments
func UploadAttachmentHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		sessionID := strings.TrimSpace(ctx.Vars().Get("session_id"))
		req := ctx.Request()
		// Cap total request body before multipart parse (DoS).
		req.Body = http.MaxBytesReader(ctx.Response(), req.Body, maxAttachmentMultipartMemory)
		if err := req.ParseMultipartForm(maxAttachmentMultipartMemory); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				return service.ErrAttachmentTooLarge
			}
			return service.ErrAttachmentBadFile
		}
		file, hdr, err := req.FormFile("file")
		if err != nil {
			return service.ErrAttachmentBadFile
		}
		defer file.Close()

		maxRead := int64(chat.MaxImageAttachmentBytes) + maxAttachmentFileReadSlack
		limited := io.LimitReader(file, maxRead+1)
		data, err := io.ReadAll(limited)
		if err != nil {
			return service.ErrAttachmentBadFile
		}
		if int64(len(data)) > maxRead {
			return service.ErrAttachmentTooLarge
		}

		filename := ""
		clientMIME := ""
		if hdr != nil {
			filename = hdr.Filename
			// Client Content-Type is only a fallback for extension-less names;
			// stored mime is always server-derived in UploadAttachment.
			clientMIME = hdr.Header.Get("Content-Type")
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chatSvc.UploadAttachment(c, sessionID, filename, clientMIME, data)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

// GetAttachmentHandler serves GET /api/v1/sessions/{session_id}/attachments/{att_id}
func GetAttachmentHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		sessionID := strings.TrimSpace(ctx.Vars().Get("session_id"))
		attID := strings.TrimSpace(ctx.Vars().Get("att_id"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chatSvc.GetAttachmentFile(c, sessionID, attID)
		})
		if err != nil {
			return err
		}
		file := out.(*service.AttachmentFile)
		w := ctx.Response()
		w.Header().Set("Content-Type", file.Mime)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		disposition := file.Disposition
		if disposition == "" {
			disposition = "inline"
			if file.Kind == string(chat.KindText) {
				disposition = "attachment"
			}
		}
		if file.Name != "" {
			w.Header().Set("Content-Disposition", disposition+`; filename="`+sanitizeDispositionFilename(file.Name)+`"`)
		} else {
			w.Header().Set("Content-Disposition", disposition)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(file.Data)
		return nil
	}
}

func sanitizeDispositionFilename(name string) string {
	name = strings.ReplaceAll(name, `"`, "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\r", "")
	return name
}

// DeleteAttachmentHandler serves DELETE /api/v1/sessions/{session_id}/attachments/{att_id}
func DeleteAttachmentHandler(chatSvc *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		sessionID := strings.TrimSpace(ctx.Vars().Get("session_id"))
		attID := strings.TrimSpace(ctx.Vars().Get("att_id"))
		_, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return nil, chatSvc.DeleteAttachment(c, sessionID, attID)
		})
		if err != nil {
			return err
		}
		ctx.Response().WriteHeader(http.StatusNoContent)
		return nil
	}
}
