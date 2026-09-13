package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/hellodeveye/postdare-go/internal/service"
	"github.com/hellodeveye/postdare-go/internal/util"
)

// UploadAttachment takes one pasted or dropped image and returns the markdown
// URL for it. The issue it belongs to is not known yet -- the image is pasted
// while the description is still being written -- so the upload stands alone
// until a saved description refers to it.
func (h *Handler) UploadAttachment(c *gin.Context) {
	// The cap is enforced on the request body as well as on the read, so an
	// oversized upload is cut off at the socket instead of being buffered whole.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.MaxAttachmentBytes+1024)
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		util.Error(c, http.StatusBadRequest, "ATTACHMENT_MISSING", "An image file is required", nil)
		return
	}
	defer file.Close()

	filename := ""
	if header != nil {
		filename = header.Filename
	}
	attachment, err := h.Service.SaveAttachment(c.Request.Context(), file, filename, currentUserID(c))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAttachmentType):
			util.Error(c, http.StatusUnsupportedMediaType, "ATTACHMENT_TYPE_UNSUPPORTED", err.Error(), nil)
		case errors.Is(err, service.ErrAttachmentEmpty):
			util.Error(c, http.StatusUnprocessableEntity, "ATTACHMENT_EMPTY", "The uploaded file is empty", nil)
		default:
			util.Error(c, http.StatusRequestEntityTooLarge, "ATTACHMENT_TOO_LARGE", err.Error(), nil)
		}
		return
	}
	util.Created(c, gin.H{
		"id":           attachment.ID,
		"filename":     attachment.Filename,
		"content_type": attachment.ContentType,
		"size":         attachment.Size,
		"url":          fmt.Sprintf("/api/v1/attachments/%d", attachment.ID),
	})
}

// GetAttachment serves an uploaded image.
//
// The response declares the type the bytes were sniffed to be at upload, with
// sniffing turned off, so the browser cannot be talked into treating the file
// as anything else. The name is only a suggestion for a save, never a path.
func (h *Handler) GetAttachment(c *gin.Context) {
	id, ok := parseUintParam(c, "attachment_id")
	if !ok {
		return
	}
	attachment, data, err := h.Service.OpenAttachment(c.Request.Context(), id)
	if err != nil {
		util.Error(c, http.StatusNotFound, "ATTACHMENT_NOT_FOUND", "Attachment not found", nil)
		return
	}
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Disposition", fmt.Sprintf("inline; filename=%q", attachment.Filename))
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	// The bytes never change once stored, so a long private cache is safe and
	// keeps a board full of screenshots from refetching on every render.
	c.Header("Cache-Control", "private, max-age=31536000, immutable")
	c.Data(http.StatusOK, attachment.ContentType, data)
}
