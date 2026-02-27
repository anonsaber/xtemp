package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
)

func buildAndVerifyStoragePath(randomID, userFilePath string) (fullPath string, targetDir string, err error) {
	targetDir = filepath.Join(config.BaseStoragePath, randomID)
	fullPath = filepath.Join(targetDir, userFilePath)
	absBasePath, _ := filepath.Abs(config.BaseStoragePath)
	absFullPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFullPath, absBasePath) {
		return "", "", errors.New("invalid filepath, attempts to escape base storage directory")
	}
	if config.StorageType == StorageLocal {
		dirToCreate := filepath.Dir(fullPath)
		if err := os.MkdirAll(dirToCreate, dirPerm); err != nil {
			return "", "", fmt.Errorf("failed to create directory %s: %w", dirToCreate, err)
		}
	}
	return fullPath, targetDir, nil
}

func saveFileContent(dstPath string, src io.Reader) (int64, error) {
	if config.StorageType == StorageR2 {
		return saveFileContentR2(dstPath, src)
	}
	file, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return 0, fmt.Errorf("failed to open file %s for writing: %w", dstPath, err)
	}
	defer file.Close()
	buf := make([]byte, bufferSize)
	written, err := io.CopyBuffer(file, src, buf)
	if err != nil {
		os.Remove(dstPath)
		return 0, fmt.Errorf("failed to write content to file %s: %w", dstPath, err)
	}
	return written, nil
}

func saveFileContentR2(dstPath string, src io.Reader) (int64, error) {
	rel, err := filepath.Rel(config.BaseStoragePath, dstPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get relative path for R2 key: %w", err)
	}
	key := filepath.ToSlash(rel)
	buf := new(bytes.Buffer)
	n, err := io.Copy(buf, src)
	if err != nil {
		return 0, fmt.Errorf("failed to read file content for R2 upload: %w", err)
	}
	upParams := &s3.PutObjectInput{
		Bucket: aws.String(r2Bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(buf.Bytes()),
	}
	_, err = s3Client.PutObject(upParams)
	if err != nil {
		return 0, fmt.Errorf("failed to upload to R2: %w", err)
	}
	return n, nil
}

func startCleanupWorker() {
	if config.RetentionSeconds <= 0 {
		logger.Printf("Cleanup worker disabled because retention is %d second(s)", config.RetentionSeconds)
		return
	}
	if config.CleanupIntervalSeconds <= 0 {
		logger.Printf("Cleanup worker disabled because interval is %d second(s)", config.CleanupIntervalSeconds)
		return
	}

	runCleanupOnce()

	ticker := time.NewTicker(time.Duration(config.CleanupIntervalSeconds) * time.Second)
	go func() {
		defer ticker.Stop()
		for range ticker.C {
			runCleanupOnce()
		}
	}()

	logger.Printf("Cleanup worker started. Interval: %ds, retention: %ds, storage: %s", config.CleanupIntervalSeconds, config.RetentionSeconds, config.StorageType)
}

func runCleanupOnce() {
	if config.StorageType == StorageR2 {
		runR2CleanupOnce()
		return
	}
	runLocalCleanupOnce()
}

func runLocalCleanupOnce() {
	entries, err := os.ReadDir(config.BaseStoragePath)
	if err != nil {
		logger.Printf("Local cleanup skipped: failed to list storage path %s: %v", config.BaseStoragePath, err)
		return
	}

	cutoff := time.Now().Add(-time.Duration(config.RetentionSeconds) * time.Second)
	for _, entry := range entries {
		targetPath := filepath.Join(config.BaseStoragePath, entry.Name())
		newest, statErr := newestModTime(targetPath)
		if statErr != nil {
			logger.Printf("Local cleanup: failed to inspect %s: %v", targetPath, statErr)
			continue
		}
		if newest.After(cutoff) {
			continue
		}
		if rmErr := os.RemoveAll(targetPath); rmErr != nil {
			logger.Printf("Local cleanup: failed to remove expired path %s: %v", targetPath, rmErr)
			continue
		}
		logger.Printf("Local cleanup: removed expired path %s", targetPath)
	}
}

func runR2CleanupOnce() {
	if s3Client == nil || r2Bucket == "" {
		logger.Printf("R2 cleanup skipped: client or bucket not initialized")
		return
	}

	cutoff := time.Now().Add(-time.Duration(config.RetentionSeconds) * time.Second)
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(r2Bucket),
	}

	err := s3Client.ListObjectsV2Pages(input, func(page *s3.ListObjectsV2Output, lastPage bool) bool {
		for _, obj := range page.Contents {
			if obj == nil || obj.Key == nil || obj.LastModified == nil {
				continue
			}
			if obj.LastModified.After(cutoff) {
				continue
			}
			_, delErr := s3Client.DeleteObject(&s3.DeleteObjectInput{
				Bucket: aws.String(r2Bucket),
				Key:    obj.Key,
			})
			if delErr != nil {
				logger.Printf("R2 cleanup: failed to delete object %s: %v", *obj.Key, delErr)
				continue
			}
			logger.Printf("R2 cleanup: deleted expired object %s", *obj.Key)
		}
		return !lastPage
	})
	if err != nil {
		logger.Printf("R2 cleanup failed: %v", err)
	}
}

func newestModTime(root string) (time.Time, error) {
	var newest time.Time
	err := filepath.Walk(root, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest, err
}
