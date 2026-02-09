package driveapi

import (
	"context"
	"fmt"
	"io"
	"os"

	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

// UploadOptions contains options for uploading a file.
type UploadOptions struct {
	FilePath     string   // Local file path to upload
	Name         string   // Name for the file in Drive (defaults to filename if empty)
	ParentID     string   // Parent folder ID (empty for root)
	MimeType     string   // MIME type (auto-detected if empty)
	UpdateFileID string   // If set, updates existing file instead of creating new
	ChunkSize    int      // Upload chunk size in bytes (default 8MB, must be multiple of 256KB)
	OnProgress   func(uploaded, total int64) // Progress callback
}

// UploadResult contains the result of an upload operation.
type UploadResult struct {
	File         *FileMetadata // Uploaded file metadata
	BytesWritten int64         // Number of bytes uploaded
}

const (
	// DefaultChunkSize is the default chunk size for resumable uploads (8MB).
	DefaultChunkSize = 8 * 1024 * 1024

	// MinChunkSize is the minimum chunk size (256KB) required by Google Drive API.
	MinChunkSize = 256 * 1024
)

// UploadFile uploads a local file to Google Drive with resumable upload support.
func (c *Client) UploadFile(ctx context.Context, opts UploadOptions) (*UploadResult, error) {
	// Validate options
	if opts.FilePath == "" {
		return nil, fmt.Errorf("file path is required")
	}

	// Open the file
	file, err := os.Open(opts.FilePath)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	// Set default name if not provided
	if opts.Name == "" {
		opts.Name = fileInfo.Name()
	}

	// Set default chunk size
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = DefaultChunkSize
	}

	// Validate chunk size (must be multiple of 256KB)
	if opts.ChunkSize%MinChunkSize != 0 {
		return nil, fmt.Errorf("chunk size must be a multiple of %d bytes", MinChunkSize)
	}

	c.logger.Info("uploading file",
		zap.String("name", opts.Name),
		zap.Int64("size", fileInfo.Size()),
		zap.String("parent_id", opts.ParentID),
		zap.Int("chunk_size", opts.ChunkSize))

	var result *UploadResult

	err = c.executeWithRetry(ctx, func() error {
		// Reset file position for retries
		if _, err := file.Seek(0, 0); err != nil {
			return fmt.Errorf("seek to start: %w", err)
		}

		// Create or update file
		if opts.UpdateFileID != "" {
			result, err = c.updateFile(ctx, file, fileInfo, opts)
		} else {
			result, err = c.createFile(ctx, file, fileInfo, opts)
		}

		return err
	})

	if err != nil {
		return nil, err
	}

	c.logger.Info("upload completed",
		zap.String("file_id", result.File.ID),
		zap.String("name", result.File.Name),
		zap.Int64("bytes_written", result.BytesWritten))

	return result, nil
}

// createFile creates a new file in Google Drive.
func (c *Client) createFile(ctx context.Context, reader io.Reader, fileInfo os.FileInfo, opts UploadOptions) (*UploadResult, error) {
	// Prepare file metadata
	driveFile := &drive.File{
		Name:     opts.Name,
		MimeType: opts.MimeType,
	}

	if opts.ParentID != "" {
		driveFile.Parents = []string{opts.ParentID}
	}

	// Create progress reader if callback provided
	var contentReader io.Reader = reader
	if opts.OnProgress != nil {
		contentReader = &progressReader{
			reader:     reader,
			total:      fileInfo.Size(),
			onProgress: opts.OnProgress,
		}
	}

	// Create resumable upload with chunk size
	call := c.svc.Files.Create(driveFile).
		Context(ctx).
		Fields("id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed").
		Media(contentReader, googleapi.ChunkSize(opts.ChunkSize), googleapi.ContentType(opts.MimeType))

	// Upload the file
	created, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}

	return &UploadResult{
		File:         convertFileToMetadata(created),
		BytesWritten: fileInfo.Size(),
	}, nil
}

// updateFile updates an existing file in Google Drive.
func (c *Client) updateFile(ctx context.Context, reader io.Reader, fileInfo os.FileInfo, opts UploadOptions) (*UploadResult, error) {
	// Prepare file metadata (only name and MIME type for updates)
	driveFile := &drive.File{
		Name:     opts.Name,
		MimeType: opts.MimeType,
	}

	// Create progress reader if callback provided
	var contentReader io.Reader = reader
	if opts.OnProgress != nil {
		contentReader = &progressReader{
			reader:     reader,
			total:      fileInfo.Size(),
			onProgress: opts.OnProgress,
		}
	}

	// Create resumable update with chunk size
	call := c.svc.Files.Update(opts.UpdateFileID, driveFile).
		Context(ctx).
		Fields("id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed").
		Media(contentReader, googleapi.ChunkSize(opts.ChunkSize), googleapi.ContentType(opts.MimeType))

	// Upload the file
	updated, err := call.Do()
	if err != nil {
		return nil, fmt.Errorf("update file: %w", err)
	}

	return &UploadResult{
		File:         convertFileToMetadata(updated),
		BytesWritten: fileInfo.Size(),
	}, nil
}

// progressReader wraps an io.Reader to track upload progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	uploaded   int64
	onProgress func(uploaded, total int64)
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.uploaded += int64(n)

	if pr.onProgress != nil {
		pr.onProgress(pr.uploaded, pr.total)
	}

	return n, err
}

// UploadStream uploads data from an io.Reader to Google Drive.
// This is useful for uploading content that's not from a file.
func (c *Client) UploadStream(ctx context.Context, reader io.Reader, size int64, opts UploadOptions) (*UploadResult, error) {
	if opts.Name == "" {
		return nil, fmt.Errorf("name is required for stream upload")
	}

	// Set default chunk size
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = DefaultChunkSize
	}

	c.logger.Info("uploading stream",
		zap.String("name", opts.Name),
		zap.Int64("size", size),
		zap.String("parent_id", opts.ParentID))

	var result *UploadResult

	err := c.executeWithRetry(ctx, func() error {
		// Prepare file metadata
		driveFile := &drive.File{
			Name:     opts.Name,
			MimeType: opts.MimeType,
		}

		if opts.ParentID != "" {
			driveFile.Parents = []string{opts.ParentID}
		}

		// Create progress reader if callback provided
		var contentReader io.Reader = reader
		if opts.OnProgress != nil {
			contentReader = &progressReader{
				reader:     reader,
				total:      size,
				onProgress: opts.OnProgress,
			}
		}

		// Create resumable upload with chunk size
		call := c.svc.Files.Create(driveFile).
			Context(ctx).
			Fields("id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed").
			Media(contentReader, googleapi.ChunkSize(opts.ChunkSize), googleapi.ContentType(opts.MimeType))

		// Upload the stream
		created, err := call.Do()
		if err != nil {
			return fmt.Errorf("upload stream: %w", err)
		}

		result = &UploadResult{
			File:         convertFileToMetadata(created),
			BytesWritten: size,
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	c.logger.Info("stream upload completed",
		zap.String("file_id", result.File.ID),
		zap.String("name", result.File.Name),
		zap.Int64("bytes_written", result.BytesWritten))

	return result, nil
}
