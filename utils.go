package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const lowercaseLetters = "abcdefghijklmnopqrstuvwxyz"
const idLength = 12

func abortWithError(c *gin.Context, statusCode int, message string, err error) {
	fullMessage := message
	if err != nil {
		fullMessage = fmt.Sprintf("%s: %v", message, err)
	}
	logger.Printf("Client Error: %s (IP: %s, Request: %s %s)", fullMessage, c.ClientIP(), c.Request.Method, c.Request.URL.Path)
	c.JSON(statusCode, gin.H{"error": message})
	c.Abort()
}

func generateUniqueID() string {
	b := make([]byte, idLength)
	_, err := rand.Read(b)
	if err != nil {
		logger.Printf("CRITICAL: crypto/rand.Read failed: %v. Using pseudo-random fallback.", err)
		fallbackResult := make([]byte, idLength)
		ts := time.Now().UnixNano()
		n := len(lowercaseLetters)
		for i := 0; i < idLength; i++ {
			fallbackResult[i] = lowercaseLetters[int((ts>>(i*4))&0xFF)%n]
		}
		return string(fallbackResult)
	}

	result := make([]byte, idLength)
	n := len(lowercaseLetters)
	for i := 0; i < idLength; i++ {
		result[i] = lowercaseLetters[int(b[i])%n]
	}

	return string(result)
}

func getSanitizedUserPath(pathParam string) (string, error) {
	cleaned := strings.Trim(pathParam, "/ ")
	if cleaned == "" {
		return "", errors.New("filepath cannot be empty")
	}
	if len(cleaned) > 255 {
		return "", errors.New("filepath segment is too long")
	}
	if strings.Contains(cleaned, "..") {
		return "", errors.New("invalid characters in filepath (path traversal attempt)")
	}
	if filepath.IsAbs(cleaned) {
		return "", errors.New("filepath must be relative")
	}
	return filepath.Clean(cleaned), nil
}

func getBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	return fmt.Sprintf("%s://%s", scheme, host)
}
