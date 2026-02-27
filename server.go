package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	if err := r.SetTrustedProxies(config.TrustedProxies); err != nil {
		logger.Fatalf("Failed to set trusted proxies: %v", err)
	}
	logger.Printf("Gin trusted proxies set to: %v", config.TrustedProxies)
	r.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; object-src 'none'; img-src 'self' data:; font-src 'self' data:;")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	})
	r.GET("/", func(c *gin.Context) {
		filePath := "./static/index.html"
		cwd, errCwd := os.Getwd()
		if errCwd != nil {
			logger.Printf("GET /: Could not get current working directory: %v", errCwd)
		} else {
			logger.Printf("GET /: Current working directory: %s", cwd)
		}
		absFilePath, errAbs := filepath.Abs(filePath)
		if errAbs != nil {
			logger.Printf("GET /: Could not resolve absolute path for %s: %v", filePath, errAbs)
		} else {
			logger.Printf("GET /: Attempting to serve index.html from absolute path: %s", absFilePath)
		}
		fileInfo, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			logger.Printf("GET /: index.html NOT FOUND at %s (resolved to %s). CWD is %s. Ensure it's copied to the container and path is correct.", filePath, absFilePath, cwd)
			c.String(http.StatusNotFound, fmt.Sprintf("Error: index.html not found. Expected at %s relative to CWD (%s).", filePath, cwd))
			return
		} else if err != nil {
			logger.Printf("GET /: Error stating index.html at %s: %v", filePath, err)
			c.String(http.StatusInternalServerError, "Internal server error checking for index.html.")
			return
		}
		if fileInfo.IsDir() {
			logger.Printf("GET /: Path %s is a directory, not a file. Cannot serve index.html.", filePath)
			c.String(http.StatusNotFound, fmt.Sprintf("Error: Expected index.html to be a file, but found a directory at %s.", filePath))
			return
		}
		logger.Printf("GET /: Serving index.html from %s", filePath)
		c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.File(filePath)
	})
	r.GET("/config/max_upload_size", handleGetMaxUploadSize)
	r.GET("/config/server_year", handleGetServerYear)
	r.GET("/config/retention_policy", handleGetRetentionPolicy)
	r.GET("/config/set_max_upload_size", handleSetMaxUploadSize)
	r.GET("/favicon.ico", func(c *gin.Context) {
		logger.Printf("GET /favicon.ico: Returning 204 No Content.")
		c.Status(http.StatusNoContent)
	})
	r.POST("/", handleUploadPost)
	r.PUT("/*filepath", handleUploadPut)
	r.GET("/:random_id/*filepath", handleDownloadFile)
	r.DELETE("/:random_id/*filepath", handleDeleteFile)
	r.MaxMultipartMemory = config.MaxUploadSize
	logger.Println("Starting XTemp File Service on :5000...")
	if err := r.Run(":5000"); err != nil {
		logger.Fatalf("Failed to start server: %v", err)
	}
}
