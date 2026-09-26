package controllers

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/xshubhamg/file-upload/models"
	"github.com/xshubhamg/file-upload/utils"
)

func UploadFile(c *gin.Context) {
	user, ok := utils.CurrentUser(c)
	if !ok {
		return
	}

	// Retrieve the file
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file provided!"})
		return
	}

	// validate file size (compare bytes directly to avoid truncation)
	if file.Size > utils.MaxUploadBytes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "File too large! Limit is 100MB"})
		return
	}

	filename, ok := utils.SafeFilename(file.Filename)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename!"})
		return
	}
	dir, err := utils.EnsureUserDir(user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fail to Create user directory!"})
		return
	}

	dst := filepath.Join(dir, filename)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Fail to save the file!"})
		return
	}

	fileModel := models.File{
		UserID:   user.ID,
		Path:     dst,
		Filename: filename,
		Size:     file.Size,
	}
	if err := utils.DB.Create(&fileModel).Error; err != nil {
		// Don't leave an orphaned file on disk when the DB write fails
		// (e.g. re-upload of the same filename hits the unique Path constraint).
		utils.RemoveFileBestEffort(dst)
		if utils.IsUniqueConstraintError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "File with this name already exists!"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create file instance"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "File uploaded successfully",
		"url":     utils.FileURL(filename),
	})
}

func ListFiles(c *gin.Context) {
	user, ok := utils.CurrentUser(c)
	if !ok {
		return
	}

	var files []models.File
	if err := utils.DB.Where("user_id = ?", user.ID).Find(&files).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error fetching files!"})
		return
	}

	c.JSON(http.StatusOK, files)
}

func GetFile(c *gin.Context) {
	user, ok := utils.CurrentUser(c)
	if !ok {
		return
	}

	filename, ok := utils.SafeFilename(c.Param("filename"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename!"})
		return
	}
	filePath := filepath.Join(utils.UserDir(user.ID), filename)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found!"})
		return
	}

	c.File(filePath)
}

func DeleteFile(c *gin.Context) {
	user, ok := utils.CurrentUser(c)
	if !ok {
		return
	}

	filename, ok := utils.SafeFilename(c.Param("filename"))
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename!"})
		return
	}

	var fileRecord models.File
	if err := utils.DB.Where("user_id = ? AND filename = ?", user.ID, filename).First(&fileRecord).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "File not found!"})
		return
	}

	// Delete it from filesystem
	filePath := filepath.Join(utils.UserDir(user.ID), filename)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete file from disk!"})
		return
	}

	// Delete reference from the DB
	if err := utils.DB.Delete(&fileRecord).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to Delete file reference from DB"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "File deleted successfully!"})
}
