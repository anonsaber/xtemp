package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gin-gonic/gin"
)

func commonUploadLogic(c *gin.Context, filename string, bodyReader io.Reader, isPut bool) {
	randomID := generateUniqueID()
	sanitizedFilename, err := getSanitizedUserPath(filename)
	if err != nil {
		abortWithError(c, http.StatusBadRequest, "Invalid filename provided", err)
		return
	}
	fullStoragePath, _, err := buildAndVerifyStoragePath(randomID, sanitizedFilename)
	if err != nil {
		abortWithError(c, http.StatusInternalServerError, "Failed to prepare storage path", err)
		return
	}
	limitedReader := io.LimitedReader{R: bodyReader, N: config.MaxUploadSize + 1}
	bytesWritten, err := saveFileContent(fullStoragePath, &limitedReader)
	if err != nil {
		abortWithError(c, http.StatusInternalServerError, "Failed to save file", err)
		return
	}
	if bytesWritten > config.MaxUploadSize {
		if config.StorageType == StorageLocal {
			os.Remove(fullStoragePath)
		}
		abortWithError(c, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("Uploaded file size (%d bytes) exceeds maximum allowed size (%d bytes)", bytesWritten, config.MaxUploadSize), nil)
		return
	}
	urlEncodedFilename := url.PathEscape(sanitizedFilename)
	accessURL := fmt.Sprintf("%s/%s/%s", getBaseURL(c.Request), randomID, urlEncodedFilename)
	deleteCommand := fmt.Sprintf("curl -X DELETE '%s'", accessURL)

	userAgent := c.GetHeader("User-Agent")
	if strings.Contains(userAgent, "curl") || strings.Contains(userAgent, "Wget") {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		c.String(http.StatusCreated,
			"\n=========================\n\n"+
				"Uploaded Success, size %d\n\n"+
				"Get File:\n\n"+
				"wget %s\n\n"+
				"Delete File:\n\n"+
				"curl -X DELETE %s\n\n"+
				"=========================\n\n",
			bytesWritten, accessURL, accessURL)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":        "File uploaded successfully",
		"id":             randomID,
		"filepath":       sanitizedFilename,
		"url":            accessURL,
		"delete_command": deleteCommand,
		"size":           bytesWritten,
	})
}

func handleUploadPost(c *gin.Context) {
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		abortWithError(c, http.StatusBadRequest, "Failed to get file from form", err)
		return
	}
	defer file.Close()
	originalFilename := header.Filename
	commonUploadLogic(c, originalFilename, file, false)
}

func handleUploadPut(c *gin.Context) {
	userPath := c.Param("filepath")
	if userPath == "" || userPath == "/" {
		abortWithError(c, http.StatusBadRequest, "Filepath for PUT cannot be empty", nil)
		return
	}
	commonUploadLogic(c, userPath, c.Request.Body, true)
}

func handleDownloadFile(c *gin.Context) {
	randomID := c.Param("random_id")
	userFilePath, err := getSanitizedUserPath(c.Param("filepath"))
	if err != nil {
		abortWithError(c, http.StatusBadRequest, "Invalid filepath in URL", err)
		return
	}
	fullStoragePath, _, err := buildAndVerifyStoragePath(randomID, userFilePath)
	if err != nil {
		abortWithError(c, http.StatusInternalServerError, "Error accessing file path", err)
		return
	}
	if config.StorageType == StorageR2 {
		rel, err := filepath.Rel(config.BaseStoragePath, fullStoragePath)
		if err != nil {
			abortWithError(c, http.StatusInternalServerError, "Failed to get R2 key", err)
			return
		}
		key := filepath.ToSlash(rel)
		getObj := &s3.GetObjectInput{
			Bucket: aws.String(r2Bucket),
			Key:    aws.String(key),
		}
		obj, err := s3Client.GetObject(getObj)
		if err != nil {
			abortWithError(c, http.StatusNotFound, "File not found", err)
			return
		}
		defer obj.Body.Close()
		downloadFilename := filepath.Base(userFilePath)
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadFilename))
		c.Header("Content-Type", "application/octet-stream")
		logger.Printf("Serving file %s for download (from R2).", key)
		io.Copy(c.Writer, obj.Body)
		return
	}
	if _, statErr := os.Stat(fullStoragePath); os.IsNotExist(statErr) {
		abortWithError(c, http.StatusNotFound, "File not found", statErr)
		return
	} else if statErr != nil {
		abortWithError(c, http.StatusInternalServerError, "Error checking file status", statErr)
		return
	}
	downloadFilename := filepath.Base(userFilePath)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadFilename))
	c.Header("Content-Type", "application/octet-stream")
	logger.Printf("Serving file %s for download.", fullStoragePath)
	c.File(fullStoragePath)
}

func handleDeleteFile(c *gin.Context) {
	randomID := c.Param("random_id")
	userFilePath, err := getSanitizedUserPath(c.Param("filepath"))
	if err != nil {
		targetErr := errors.New("filepath cannot be empty")
		if err.Error() == targetErr.Error() {
			userFilePath = ""
		} else {
			abortWithError(c, http.StatusBadRequest, "Invalid filepath for deletion", err)
			return
		}
	}
	var pathToOperateOn string
	var operationDescription string
	var r2Key string
	if userFilePath == "" {
		_, dirPath, errBuild := buildAndVerifyStoragePath(randomID, ".")
		if errBuild != nil {
			abortWithError(c, http.StatusInternalServerError, "Error accessing directory path for deletion", errBuild)
			return
		}
		pathToOperateOn = dirPath
		operationDescription = fmt.Sprintf("directory %s and all its contents", dirPath)
		absBasePath, _ := filepath.Abs(config.BaseStoragePath)
		absPathToOperate, _ := filepath.Abs(pathToOperateOn)
		if absPathToOperate == absBasePath {
			abortWithError(c, http.StatusForbidden, "Cannot delete base storage directory", nil)
			return
		}
		rel, _ := filepath.Rel(config.BaseStoragePath, pathToOperateOn)
		r2Key = filepath.ToSlash(rel)
	} else {
		fullStoragePath, _, errBuild := buildAndVerifyStoragePath(randomID, userFilePath)
		if errBuild != nil {
			abortWithError(c, http.StatusInternalServerError, "Error accessing file path for deletion", errBuild)
			return
		}
		pathToOperateOn = fullStoragePath
		operationDescription = fmt.Sprintf("file %s", userFilePath)
		rel, _ := filepath.Rel(config.BaseStoragePath, pathToOperateOn)
		r2Key = filepath.ToSlash(rel)
	}
	if config.StorageType == StorageR2 {
		if userFilePath == "" {
			prefix := randomID + "/"
			listInput := &s3.ListObjectsV2Input{
				Bucket: aws.String(r2Bucket),
				Prefix: aws.String(prefix),
			}
			err := s3Client.ListObjectsV2Pages(listInput, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
				for _, obj := range page.Contents {
					_, delErr := s3Client.DeleteObject(&s3.DeleteObjectInput{
						Bucket: aws.String(r2Bucket),
						Key:    obj.Key,
					})
					if delErr != nil {
						logger.Printf("Failed to delete object %s: %v", *obj.Key, delErr)
					}
				}
				return !lastPage
			})
			if err != nil {
				abortWithError(c, http.StatusInternalServerError, "Failed to delete directory in R2", err)
				return
			}
			logger.Printf("Successfully deleted directory %s and all its contents (R2).", prefix)
		} else {
			_, err := s3Client.DeleteObject(&s3.DeleteObjectInput{
				Bucket: aws.String(r2Bucket),
				Key:    aws.String(r2Key),
			})
			if err != nil {
				abortWithError(c, http.StatusInternalServerError, "Failed to delete file in R2", err)
				return
			}
			logger.Printf("Successfully deleted file %s (R2).", r2Key)
		}
		c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Successfully deleted %s", userFilePath)})
		return
	}
	if _, statErr := os.Stat(pathToOperateOn); os.IsNotExist(statErr) {
		abortWithError(c, http.StatusNotFound, fmt.Sprintf("Path %s not found for deletion", userFilePath), statErr)
		return
	} else if statErr != nil {
		abortWithError(c, http.StatusInternalServerError, "Error checking path status for deletion", statErr)
		return
	}
	if err := os.RemoveAll(pathToOperateOn); err != nil {
		abortWithError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to delete %s", operationDescription), err)
		return
	}
	logger.Printf("Successfully deleted %s.", operationDescription)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("Successfully deleted %s", userFilePath)})
}

func handleGetMaxUploadSize(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"max_upload_size": config.MaxUploadSize,
	})
}

func handleSetMaxUploadSize(c *gin.Context) {
	password := c.Query("password")
	expected := os.Getenv(envConfigAPIPassword)
	if expected == "" || password != expected {
		abortWithError(c, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	sizeStr := c.Query("size")
	if sizeStr == "" {
		abortWithError(c, http.StatusBadRequest, "Missing size parameter", nil)
		return
	}
	size, err := strconv.ParseInt(sizeStr, 10, 64)
	if err != nil || size <= 0 {
		abortWithError(c, http.StatusBadRequest, "Invalid size value", err)
		return
	}
	config.MaxUploadSize = size
	c.JSON(http.StatusOK, gin.H{
		"message":         "Max upload size updated",
		"max_upload_size": config.MaxUploadSize,
	})
}

func handleGetServerYear(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"year": time.Now().Year(),
	})
}

func handleGetRetentionPolicy(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"retention_seconds": config.RetentionSeconds,
		"storage_type":      config.StorageType,
		"auto_cleanup":      true,
	})
}
