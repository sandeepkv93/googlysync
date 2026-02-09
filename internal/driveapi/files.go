package driveapi

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
)

// FileMetadata represents metadata for a file or folder in Google Drive.
type FileMetadata struct {
	ID           string    // Drive file ID
	Name         string    // File name
	MimeType     string    // MIME type
	ModifiedTime time.Time // Last modified time
	Size         int64     // File size in bytes (0 for folders)
	MD5Checksum  string    // MD5 checksum (empty for folders and Google Workspace files)
	Parents      []string  // Parent folder IDs
	Trashed      bool      // Whether the file is in trash
}

// ListOptions contains options for listing files.
type ListOptions struct {
	Query        string // Drive API query string (e.g., "name contains 'test'")
	PageSize     int    // Number of files per page (max 1000, default 100)
	PageToken    string // Token for pagination
	OrderBy      string // Order by clause (e.g., "modifiedTime desc")
	Fields       string // Fields to include in response
	IncludeTrashed bool // Whether to include trashed files
}

// ListResult contains the result of a file listing operation.
type ListResult struct {
	Files         []*FileMetadata // Files in the current page
	NextPageToken string          // Token for next page (empty if no more pages)
}

// DefaultFields returns the default fields to request for file metadata.
// This minimizes data transfer while getting all essential information.
const DefaultFields = "nextPageToken, files(id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed)"

// ListFiles lists files in Google Drive with the given options.
func (c *Client) ListFiles(ctx context.Context, opts ListOptions) (*ListResult, error) {
	var result *ListResult

	err := c.executeWithRetry(ctx, func() error {
		// Set default values
		if opts.PageSize <= 0 {
			opts.PageSize = 100
		}
		if opts.PageSize > 1000 {
			opts.PageSize = 1000
		}
		if opts.Fields == "" {
			opts.Fields = DefaultFields
		}

		// Build the request
		req := c.svc.Files.List().
			PageSize(int64(opts.PageSize))

		if opts.Fields != "" {
			req = req.Fields(googleapi.Field(opts.Fields))
		}

		if opts.Query != "" {
			req = req.Q(opts.Query)
		}
		if opts.PageToken != "" {
			req = req.PageToken(opts.PageToken)
		}
		if opts.OrderBy != "" {
			req = req.OrderBy(opts.OrderBy)
		}
		if !opts.IncludeTrashed {
			req = req.Q("trashed = false")
		}

		// Execute the request
		resp, err := req.Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("list files: %w", err)
		}

		// Convert response to our types
		files := make([]*FileMetadata, len(resp.Files))
		for i, f := range resp.Files {
			files[i] = convertFileToMetadata(f)
		}

		result = &ListResult{
			Files:         files,
			NextPageToken: resp.NextPageToken,
		}

		c.logger.Debug("listed files",
			zap.Int("count", len(files)),
			zap.String("next_page_token", resp.NextPageToken))

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetFile retrieves metadata for a specific file by ID.
func (c *Client) GetFile(ctx context.Context, fileID string) (*FileMetadata, error) {
	var metadata *FileMetadata

	err := c.executeWithRetry(ctx, func() error {
		file, err := c.svc.Files.Get(fileID).
			Fields("id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("get file %s: %w", fileID, err)
		}

		metadata = convertFileToMetadata(file)

		c.logger.Debug("retrieved file metadata",
			zap.String("file_id", fileID),
			zap.String("name", metadata.Name))

		return nil
	})

	if err != nil {
		return nil, err
	}

	return metadata, nil
}

// CreateFolder creates a new folder in Google Drive.
func (c *Client) CreateFolder(ctx context.Context, name, parentID string) (*FileMetadata, error) {
	var metadata *FileMetadata

	err := c.executeWithRetry(ctx, func() error {
		file := &drive.File{
			Name:     name,
			MimeType: "application/vnd.google-apps.folder",
		}

		if parentID != "" {
			file.Parents = []string{parentID}
		}

		created, err := c.svc.Files.Create(file).
			Fields("id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("create folder %s: %w", name, err)
		}

		metadata = convertFileToMetadata(created)

		c.logger.Info("created folder",
			zap.String("folder_id", metadata.ID),
			zap.String("name", name),
			zap.String("parent_id", parentID))

		return nil
	})

	if err != nil {
		return nil, err
	}

	return metadata, nil
}

// DeleteFile moves a file to trash (soft delete).
// To permanently delete, use PermanentlyDeleteFile.
func (c *Client) DeleteFile(ctx context.Context, fileID string) error {
	err := c.executeWithRetry(ctx, func() error {
		// Move to trash
		_, err := c.svc.Files.Update(fileID, &drive.File{
			Trashed: true,
		}).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("delete file %s: %w", fileID, err)
		}

		c.logger.Info("moved file to trash", zap.String("file_id", fileID))
		return nil
	})

	return err
}

// PermanentlyDeleteFile permanently deletes a file from Google Drive.
// This operation cannot be undone.
func (c *Client) PermanentlyDeleteFile(ctx context.Context, fileID string) error {
	err := c.executeWithRetry(ctx, func() error {
		err := c.svc.Files.Delete(fileID).Context(ctx).Do()
		if err != nil {
			return fmt.Errorf("permanently delete file %s: %w", fileID, err)
		}

		c.logger.Warn("permanently deleted file", zap.String("file_id", fileID))
		return nil
	})

	return err
}

// ListFilesRecursive lists all files recursively starting from the given folder ID.
// If folderID is empty or "root", it starts from the root folder.
// This method handles pagination automatically and returns all files.
func (c *Client) ListFilesRecursive(ctx context.Context, folderID string) ([]*FileMetadata, error) {
	if folderID == "" {
		folderID = "root"
	}

	var allFiles []*FileMetadata

	// Start with the root folder
	queue := []string{folderID}

	for len(queue) > 0 {
		// Pop the first folder from the queue
		currentFolderID := queue[0]
		queue = queue[1:]

		// List all children of the current folder
		query := fmt.Sprintf("'%s' in parents and trashed = false", currentFolderID)
		pageToken := ""

		for {
			result, err := c.ListFiles(ctx, ListOptions{
				Query:     query,
				PageSize:  1000,
				PageToken: pageToken,
			})
			if err != nil {
				return nil, fmt.Errorf("list files in folder %s: %w", currentFolderID, err)
			}

			for _, file := range result.Files {
				allFiles = append(allFiles, file)

				// If it's a folder, add to queue for recursive listing
				if file.MimeType == "application/vnd.google-apps.folder" {
					queue = append(queue, file.ID)
				}
			}

			// Check if there are more pages
			if result.NextPageToken == "" {
				break
			}
			pageToken = result.NextPageToken
		}
	}

	c.logger.Info("recursively listed all files",
		zap.String("root_folder_id", folderID),
		zap.Int("total_files", len(allFiles)))

	return allFiles, nil
}

// UpdateFile updates file metadata.
func (c *Client) UpdateFile(ctx context.Context, fileID string, update *drive.File) (*FileMetadata, error) {
	var metadata *FileMetadata

	err := c.executeWithRetry(ctx, func() error {
		updated, err := c.svc.Files.Update(fileID, update).
			Fields("id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed").
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("update file %s: %w", fileID, err)
		}

		metadata = convertFileToMetadata(updated)

		c.logger.Debug("updated file metadata",
			zap.String("file_id", fileID))

		return nil
	})

	if err != nil {
		return nil, err
	}

	return metadata, nil
}

// convertFileToMetadata converts a Drive API File to our FileMetadata type.
func convertFileToMetadata(f *drive.File) *FileMetadata {
	var modifiedTime time.Time
	if f.ModifiedTime != "" {
		modifiedTime, _ = time.Parse(time.RFC3339, f.ModifiedTime)
	}

	return &FileMetadata{
		ID:           f.Id,
		Name:         f.Name,
		MimeType:     f.MimeType,
		ModifiedTime: modifiedTime,
		Size:         f.Size,
		MD5Checksum:  f.Md5Checksum,
		Parents:      f.Parents,
		Trashed:      f.Trashed,
	}
}
