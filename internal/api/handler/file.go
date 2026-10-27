package handler

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/api/response"
	"github.com/xshubhamg/file-upload/internal/apperr"
	"github.com/xshubhamg/file-upload/internal/config"
	"github.com/xshubhamg/file-upload/internal/domain"
	"github.com/xshubhamg/file-upload/internal/service"
	"github.com/xshubhamg/file-upload/internal/storage"
)

type FileHandler struct {
	svc *service.FileService
	cfg config.Config
}

func NewFileHandler(svc *service.FileService, cfg config.Config) *FileHandler {
	return &FileHandler{svc: svc, cfg: cfg}
}

type initiateRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type initiateResponse struct {
	File   FileDTO                 `json:"file"`
	Upload storage.PresignedUpload `json:"upload"`
}

func (h *FileHandler) Initiate(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	var req initiateRequest
	if !response.BindJSON(c, &req) {
		return
	}
	file, upload, err := h.svc.InitiateUpload(c.Request.Context(), userID, service.InitiateUploadInput{
		Filename:    req.Filename,
		ContentType: req.ContentType,
		SizeBytes:   req.SizeBytes,
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusCreated, initiateResponse{File: NewFileDTO(file), Upload: upload})
}

func (h *FileHandler) Complete(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	fileID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	file, err := h.svc.CompleteUpload(c.Request.Context(), userID, fileID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusOK, gin.H{"file": NewFileDTO(file)})
}

func (h *FileHandler) List(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	limit, ok := parseLimit(c, 20, 100)
	if !ok {
		return
	}
	page, err := h.svc.List(c.Request.Context(), userID, c.Query("cursor"), limit)
	if err != nil {
		response.Error(c, err)
		return
	}

	items := make([]FileDTO, 0, len(page.Items))
	for _, file := range page.Items {
		items = append(items, NewFileDTO(file))
	}
	response.JSON(c, http.StatusOK, gin.H{
		"items":       items,
		"next_cursor": page.NextCursor,
	})
}

func (h *FileHandler) Get(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	fileID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	file, url, err := h.svc.Get(c.Request.Context(), userID, fileID)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusOK, gin.H{
		"file":         NewFileDTO(file),
		"download_url": url,
	})
}

func (h *FileHandler) Download(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	fileID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	_, url, err := h.svc.Download(c.Request.Context(), userID, fileID)
	if err != nil {
		response.Error(c, err)
		return
	}
	c.Redirect(http.StatusFound, url)
}

func (h *FileHandler) Delete(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	fileID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID, fileID); err != nil {
		response.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *FileHandler) LegacyUpload(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		if response.IsBodyTooLarge(err) || strings.Contains(err.Error(), "too large") {
			response.Error(c, apperr.PayloadTooLarge("file exceeds the upload limit"))
			return
		}
		response.Error(c, apperr.Validation("no file provided").
			WithDetail("file", "multipart field \"file\" is required"))
		return
	}
	if fileHeader.Size > h.cfg.MaxUploadBytes {
		response.Error(c, apperr.PayloadTooLarge("file exceeds the upload limit"))
		return
	}

	opened, err := fileHeader.Open()
	if err != nil {
		response.Error(c, apperr.Internal(err))
		return
	}
	defer opened.Close()

	contentType := fileHeader.Header.Get("Content-Type")
	if contentType == "" || strings.EqualFold(contentType, "application/octet-stream") {
		buf := make([]byte, 512)
		n, readErr := io.ReadFull(opened, buf)
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			response.Error(c, apperr.Internal(readErr))
			return
		}
		if _, seekErr := opened.Seek(0, io.SeekStart); seekErr != nil {
			response.Error(c, apperr.Internal(seekErr))
			return
		}
		contentType = http.DetectContentType(buf[:n])
	}

	file, err := h.svc.UploadLegacy(c.Request.Context(), userID, fileHeader.Filename, contentType, fileHeader.Size, opened)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusCreated, gin.H{
		"message": "File uploaded successfully",
		"url":     "/api/file/" + file.Filename,
		"file":    NewFileDTO(file),
	})
}

func (h *FileHandler) LegacyList(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	// The legacy endpoint returned every file. Walk pages internally while
	// keeping the old array response shape; cap the walk to avoid unbounded work.
	files := make([]legacyFileDTO, 0)
	cursor := ""
	for page := 0; page < 100; page++ {
		result, err := h.svc.List(c.Request.Context(), userID, cursor, 100)
		if err != nil {
			response.Error(c, err)
			return
		}
		for _, file := range result.Items {
			files = append(files, newLegacyFileDTO(file))
		}
		if result.NextCursor == "" {
			break
		}
		cursor = result.NextCursor
	}
	response.JSON(c, http.StatusOK, files)
}

func (h *FileHandler) LegacyGet(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	filename, err := service.SafeFilename(c.Param("filename"))
	if err != nil {
		response.Error(c, err)
		return
	}
	file, err := h.svc.FindByFilename(c.Request.Context(), userID, filename)
	if err != nil {
		response.Error(c, err)
		return
	}
	if file.Status != domain.FileActive {
		response.Error(c, apperr.Conflict("UPLOAD_NOT_COMPLETED", "file upload has not been completed"))
		return
	}
	reader, info, err := h.svc.Open(c.Request.Context(), file)
	if err != nil {
		response.Error(c, err)
		return
	}
	defer reader.Close()

	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": file.Filename})
	c.DataFromReader(http.StatusOK, info.Size, file.ContentType, reader, map[string]string{
		"Content-Disposition": disposition,
	})
}

func (h *FileHandler) LegacyDelete(c *gin.Context) {
	userID, ok := CurrentUserID(c)
	if !ok {
		return
	}
	filename, err := service.SafeFilename(c.Param("filename"))
	if err != nil {
		response.Error(c, err)
		return
	}
	file, err := h.svc.FindByFilename(c.Request.Context(), userID, filename)
	if err != nil {
		response.Error(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), userID, file.ID); err != nil {
		response.Error(c, err)
		return
	}
	response.JSON(c, http.StatusOK, gin.H{"message": "File deleted successfully!"})
}
