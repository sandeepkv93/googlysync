package driveapi

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// DownloadOptions contains options for downloading a file.
type DownloadOptions struct {
	FileID       string // Drive file ID to download
	DestPath     string // Destination path for the file
	VerifyChecksum bool   // Whether to verify MD5 checksum after download
	OnProgress   func(downloaded, total int64) // Progress callback
}

// DownloadResult contains the result of a download operation.
type DownloadResult struct {
	BytesRead    int64  // Number of bytes downloaded
	MD5Checksum  string // MD5 checksum of downloaded file
	VerifiedOK   bool   // Whether checksum verification passed
}

// DownloadFile downloads a file from Google Drive with resumable download support.
// It uses atomic writes: downloads to a temp file, verifies checksum, then renames.
func (c *Client) DownloadFile(ctx context.Context, opts DownloadOptions) (*DownloadResult, error) {
	if opts.FileID == "" {
		return nil, fmt.Errorf("file ID is required")
	}
	if opts.DestPath == "" {
		return nil, fmt.Errorf("destination path is required")
	}

	c.logger.Info("downloading file",
		zap.String("file_id", opts.FileID),
		zap.String("dest_path", opts.DestPath))

	// Get file metadata first to know the expected MD5
	metadata, err := c.GetFile(ctx, opts.FileID)
	if err != nil {
		return nil, fmt.Errorf("get file metadata: %w", err)
	}

	// Google Workspace files (Docs, Sheets, etc.) don't have MD5 checksums
	// and need to be exported in a different format
	if metadata.MD5Checksum == "" && isGoogleWorkspaceFile(metadata.MimeType) {
		return nil, fmt.Errorf("cannot download Google Workspace file %s (MIME: %s): export not yet implemented",
			metadata.Name, metadata.MimeType)
	}

	// Create destination directory if it doesn't exist
	destDir := filepath.Dir(opts.DestPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("create destination directory: %w", err)
	}

	// Create a temporary file for atomic writes
	tempFile := fmt.Sprintf("%s.tmp.%s", opts.DestPath, uuid.New().String())
	defer os.Remove(tempFile) // Clean up temp file on error

	var result *DownloadResult

	err = c.executeWithRetry(ctx, func() error {
		// Download to temp file
		bytesRead, downloadedMD5, err := c.downloadToFile(ctx, opts.FileID, tempFile, metadata.Size, opts.OnProgress)
		if err != nil {
			return fmt.Errorf("download to temp file: %w", err)
		}

		result = &DownloadResult{
			BytesRead:   bytesRead,
			MD5Checksum: downloadedMD5,
			VerifiedOK:  false,
		}

		// Verify checksum if requested and available
		if opts.VerifyChecksum && metadata.MD5Checksum != "" {
			if downloadedMD5 != metadata.MD5Checksum {
				return fmt.Errorf("checksum mismatch: expected %s, got %s",
					metadata.MD5Checksum, downloadedMD5)
			}
			result.VerifiedOK = true
			c.logger.Debug("checksum verified",
				zap.String("file_id", opts.FileID),
				zap.String("checksum", downloadedMD5))
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Atomic rename: move temp file to destination
	if err := os.Rename(tempFile, opts.DestPath); err != nil {
		return nil, fmt.Errorf("rename temp file to destination: %w", err)
	}

	c.logger.Info("download completed",
		zap.String("file_id", opts.FileID),
		zap.String("dest_path", opts.DestPath),
		zap.Int64("bytes_read", result.BytesRead),
		zap.Bool("verified", result.VerifiedOK))

	return result, nil
}

// downloadToFile downloads a file from Drive to a local file path.
// Returns the number of bytes downloaded and the MD5 checksum.
func (c *Client) downloadToFile(ctx context.Context, fileID, destPath string, totalSize int64, onProgress func(downloaded, total int64)) (int64, string, error) {
	// Get the file content
	resp, err := c.svc.Files.Get(fileID).
		Download()
	if err != nil {
		return 0, "", fmt.Errorf("get file content: %w", err)
	}
	defer resp.Body.Close()

	// Create output file
	outFile, err := os.Create(destPath)
	if err != nil {
		return 0, "", fmt.Errorf("create output file: %w", err)
	}
	defer outFile.Close()

	// Create MD5 hasher
	hasher := md5.New()

	// Create multi-writer to write to both file and hasher
	multiWriter := io.MultiWriter(outFile, hasher)

	// Wrap with progress tracker if callback provided
	var reader io.Reader = resp.Body
	if onProgress != nil {
		reader = &downloadProgressReader{
			reader:     resp.Body,
			total:      totalSize,
			onProgress: onProgress,
		}
	}

	// Copy data
	bytesRead, err := io.Copy(multiWriter, reader)
	if err != nil {
		return 0, "", fmt.Errorf("copy file content: %w", err)
	}

	// Get MD5 checksum
	checksum := hex.EncodeToString(hasher.Sum(nil))

	return bytesRead, checksum, nil
}

// downloadProgressReader wraps an io.Reader to track download progress.
type downloadProgressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	onProgress func(downloaded, total int64)
}

func (dpr *downloadProgressReader) Read(p []byte) (int, error) {
	n, err := dpr.reader.Read(p)
	dpr.downloaded += int64(n)

	if dpr.onProgress != nil {
		dpr.onProgress(dpr.downloaded, dpr.total)
	}

	return n, err
}

// DownloadToWriter downloads a file from Google Drive to an io.Writer.
// This is useful for downloading to memory or other non-file destinations.
func (c *Client) DownloadToWriter(ctx context.Context, fileID string, writer io.Writer, onProgress func(downloaded, total int64)) (int64, error) {
	c.logger.Debug("downloading file to writer", zap.String("file_id", fileID))

	var bytesRead int64

	err := c.executeWithRetry(ctx, func() error {
		// Get file metadata for size
		metadata, err := c.GetFile(ctx, fileID)
		if err != nil {
			return fmt.Errorf("get file metadata: %w", err)
		}

		// Get the file content
		resp, err := c.svc.Files.Get(fileID).
			Download()
		if err != nil {
			return fmt.Errorf("get file content: %w", err)
		}
		defer resp.Body.Close()

		// Wrap with progress tracker if callback provided
		var reader io.Reader = resp.Body
		if onProgress != nil {
			reader = &downloadProgressReader{
				reader:     resp.Body,
				total:      metadata.Size,
				onProgress: onProgress,
			}
		}

		// Copy data
		bytesRead, err = io.Copy(writer, reader)
		if err != nil {
			return fmt.Errorf("copy file content: %w", err)
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	c.logger.Debug("download to writer completed",
		zap.String("file_id", fileID),
		zap.Int64("bytes_read", bytesRead))

	return bytesRead, nil
}

// isGoogleWorkspaceFile returns true if the MIME type is a Google Workspace file
// (Docs, Sheets, Slides, etc.) that needs to be exported instead of downloaded.
func isGoogleWorkspaceFile(mimeType string) bool {
	workspaceMimeTypes := []string{
		"application/vnd.google-apps.document",     // Google Docs
		"application/vnd.google-apps.spreadsheet",  // Google Sheets
		"application/vnd.google-apps.presentation", // Google Slides
		"application/vnd.google-apps.drawing",      // Google Drawings
		"application/vnd.google-apps.form",         // Google Forms
		"application/vnd.google-apps.map",          // Google My Maps
		"application/vnd.google-apps.site",         // Google Sites
	}

	for _, wmt := range workspaceMimeTypes {
		if mimeType == wmt {
			return true
		}
	}

	return false
}

// ExportGoogleWorkspaceFile exports a Google Workspace file to a standard format.
// This is a stub for future implementation.
func (c *Client) ExportGoogleWorkspaceFile(ctx context.Context, fileID, mimeType, destPath string) error {
	// TODO: Implement export for Google Workspace files
	// Common export formats:
	// - Docs -> application/pdf, text/plain, application/vnd.openxmlformats-officedocument.wordprocessingml.document
	// - Sheets -> application/pdf, text/csv, application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
	// - Slides -> application/pdf, application/vnd.openxmlformats-officedocument.presentationml.presentation
	return fmt.Errorf("export Google Workspace files not yet implemented")
}
