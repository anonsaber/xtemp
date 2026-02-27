package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
