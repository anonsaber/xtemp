package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	envBaseStoragePath   = "XTEMP_STORAGE_PATH"
	envTrustedProxies    = "TRUSTED_PROXIES"
	envMaxUploadSize     = "MAX_UPLOAD_SIZE"
	envRetentionSeconds  = "XTEMP_RETENTION_SECONDS"
	envCleanupInterval   = "XTEMP_CLEANUP_INTERVAL_SECONDS"
	envStorageType       = "STORAGE_TYPE"
	envR2AccountID       = "R2_ACCOUNT_ID"
	envR2AccessKeyID     = "R2_ACCESS_KEY_ID"
	envR2SecretAccessKey = "R2_SECRET_ACCESS_KEY"
	envR2BucketName      = "R2_BUCKET_NAME"
	envConfigAPIPassword = "XTEMP_CONFIG_API_PASSWORD"

	defaultStoragePath   = "/var/lib/xtemp-store"
	defaultMaxUploadSize = 50 << 20
	defaultRetentionSeconds int64 = 24 * 3600
	defaultCleanupInterval  int64 = 3600
	bufferSize           = 16 * 1024

	dirPerm  os.FileMode = 0750
	filePerm os.FileMode = 0640
)

type StorageType string

const (
	StorageLocal StorageType = "local"
	StorageR2    StorageType = "r2"
)

type AppConfig struct {
	BaseStoragePath   string
	MaxUploadSize     int64
	RetentionSeconds       int64
	CleanupIntervalSeconds int64
	TrustedProxies    []string
	StorageType       StorageType
	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2BucketName      string
}

var (
	logger   *log.Logger
	config   *AppConfig
	s3Client *s3.S3
	r2Bucket string
)

func init() {
	logger = log.New(os.Stdout, "xtemp_app: ", log.Ldate|log.Ltime|log.Lshortfile)
	config = loadConfig()

	switch config.StorageType {
	case StorageLocal:
		if err := os.MkdirAll(config.BaseStoragePath, dirPerm); err != nil {
			logger.Fatalf("Could not create base storage directory %s: %v", config.BaseStoragePath, err)
		}
		logger.Printf("Base storage directory %s ensured with permissions %o", config.BaseStoragePath, dirPerm)
	case StorageR2:
		endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", config.R2AccountID)
		sess, err := session.NewSession(&aws.Config{
			Region:           aws.String("auto"),
			Endpoint:         aws.String(endpoint),
			S3ForcePathStyle: aws.Bool(true),
			Credentials:      credentials.NewStaticCredentials(config.R2AccessKeyID, config.R2SecretAccessKey, ""),
		})
		if err != nil {
			logger.Fatalf("Failed to create R2 session: %v", err)
		}
		s3Client = s3.New(sess)
		r2Bucket = config.R2BucketName
		logger.Printf("R2 client initialized for endpoint %s, bucket %s", endpoint, r2Bucket)
	}

	logger.Printf("Max upload size set to %d bytes (%dMB)", config.MaxUploadSize, config.MaxUploadSize/(1<<20))
	logger.Printf("Retention period set to %s", time.Duration(config.RetentionSeconds)*time.Second)
	logger.Printf("Cleanup interval set to %s", time.Duration(config.CleanupIntervalSeconds)*time.Second)
	logger.Printf("Trusted proxies configured: %v", config.TrustedProxies)
	logger.Printf("Storage type: %s", config.StorageType)

	startCleanupWorker()
}

func loadConfig() *AppConfig {
	cfg := &AppConfig{
		BaseStoragePath:        defaultStoragePath,
		MaxUploadSize:          defaultMaxUploadSize,
		RetentionSeconds:       defaultRetentionSeconds,
		CleanupIntervalSeconds: defaultCleanupInterval,
		TrustedProxies:         []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"},
		StorageType:            StorageLocal,
	}
	if path := os.Getenv(envBaseStoragePath); path != "" {
		cfg.BaseStoragePath = filepath.Clean(path)
	}
	if sizeStr := os.Getenv(envMaxUploadSize); sizeStr != "" {
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err == nil && size > 0 {
			cfg.MaxUploadSize = size
		} else {
			logger.Printf("Invalid %s value '%s', using default %dMB", envMaxUploadSize, sizeStr, defaultMaxUploadSize/(1<<20))
		}
	}
	if retentionSecondsStr := os.Getenv(envRetentionSeconds); retentionSecondsStr != "" {
		retentionSeconds, err := strconv.ParseInt(retentionSecondsStr, 10, 64)
		if err == nil && retentionSeconds > 0 {
			cfg.RetentionSeconds = retentionSeconds
		} else {
			logger.Printf("Invalid %s value '%s', using default %d second(s)", envRetentionSeconds, retentionSecondsStr, defaultRetentionSeconds)
		}
	}
	if cleanupIntervalStr := os.Getenv(envCleanupInterval); cleanupIntervalStr != "" {
		cleanupInterval, err := strconv.ParseInt(cleanupIntervalStr, 10, 64)
		if err == nil && cleanupInterval > 0 {
			cfg.CleanupIntervalSeconds = cleanupInterval
		} else {
			logger.Printf("Invalid %s value '%s', using default %d second(s)", envCleanupInterval, cleanupIntervalStr, defaultCleanupInterval)
		}
	}
	if proxyStr := os.Getenv(envTrustedProxies); proxyStr != "" {
		proxies := strings.Split(proxyStr, ",")
		validProxies := make([]string, 0, len(proxies))
		for _, p := range proxies {
			trimmed := strings.TrimSpace(p)
			if _, _, err := net.ParseCIDR(trimmed); err == nil {
				validProxies = append(validProxies, trimmed)
			} else if ip := net.ParseIP(trimmed); ip != nil {
				validProxies = append(validProxies, trimmed)
			} else if trimmed != "" {
				logger.Printf("Invalid proxy format in TRUSTED_PROXIES: %s", trimmed)
			}
		}
		if len(validProxies) > 0 {
			cfg.TrustedProxies = validProxies
		} else {
			logger.Println("No valid proxies found in TRUSTED_PROXIES, using default.")
		}
	}
	if st := os.Getenv(envStorageType); st != "" {
		st = strings.ToLower(strings.TrimSpace(st))
		if st == string(StorageLocal) || st == string(StorageR2) {
			cfg.StorageType = StorageType(st)
		} else {
			logger.Printf("Invalid STORAGE_TYPE '%s', using default 'local'", st)
		}
	}
	cfg.R2AccountID = os.Getenv(envR2AccountID)
	cfg.R2AccessKeyID = os.Getenv(envR2AccessKeyID)
	cfg.R2SecretAccessKey = os.Getenv(envR2SecretAccessKey)
	cfg.R2BucketName = os.Getenv(envR2BucketName)
	return cfg
}
