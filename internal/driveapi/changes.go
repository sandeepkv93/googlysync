package driveapi

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// Change represents a change to a file in Google Drive.
type Change struct {
	ChangeID string        // Change ID (for tracking)
	FileID   string        // File ID that changed
	File     *FileMetadata // File metadata (nil if deleted)
	Removed  bool          // Whether the file was removed
}

// ChangesResult contains the result of a changes listing operation.
type ChangesResult struct {
	Changes          []*Change // Changes in the current page
	NewStartPageToken string    // New start page token for next poll
	NextPageToken    string    // Token for next page (for same start page token)
}

// GetStartPageToken retrieves the start page token for the Changes API.
// This token represents the current state and should be used for future change polling.
func (c *Client) GetStartPageToken(ctx context.Context) (string, error) {
	var token string

	err := c.executeWithRetry(ctx, func() error {
		resp, err := c.svc.Changes.GetStartPageToken().
			Context(ctx).
			Do()
		if err != nil {
			return fmt.Errorf("get start page token: %w", err)
		}

		token = resp.StartPageToken

		c.logger.Debug("retrieved start page token",
			zap.String("token", token))

		return nil
	})

	if err != nil {
		return "", err
	}

	return token, nil
}

// ListChanges lists changes since the given page token.
// The page token should be obtained from GetStartPageToken or from a previous ListChanges call.
func (c *Client) ListChanges(ctx context.Context, pageToken string, pageSize int) (*ChangesResult, error) {
	if pageToken == "" {
		return nil, fmt.Errorf("page token is required")
	}

	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var result *ChangesResult

	err := c.executeWithRetry(ctx, func() error {
		req := c.svc.Changes.List(pageToken).
			PageSize(int64(pageSize)).
			RestrictToMyDrive(true). // Only sync files in "My Drive", not Shared Drives
			Fields("nextPageToken, newStartPageToken, changes(changeId, fileId, removed, file(id, name, mimeType, modifiedTime, size, md5Checksum, parents, trashed))").
			Context(ctx)

		resp, err := req.Do()
		if err != nil {
			return fmt.Errorf("list changes: %w", err)
		}

		// Convert response to our types
		changes := make([]*Change, len(resp.Changes))
		for i, c := range resp.Changes {
			change := &Change{
				ChangeID: c.FileId, // Use FileId as the change identifier
				FileID:   c.FileId,
				Removed:  c.Removed,
			}

			// Only populate file metadata if not removed
			if !c.Removed && c.File != nil {
				change.File = convertFileToMetadata(c.File)
			}

			changes[i] = change
		}

		result = &ChangesResult{
			Changes:          changes,
			NewStartPageToken: resp.NewStartPageToken,
			NextPageToken:    resp.NextPageToken,
		}

		c.logger.Debug("listed changes",
			zap.Int("count", len(changes)),
			zap.String("new_start_page_token", resp.NewStartPageToken),
			zap.String("next_page_token", resp.NextPageToken))

		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// ListAllChanges lists all changes since the given page token.
// This is a convenience method that handles pagination automatically.
// It returns all changes and the new start page token for the next poll.
func (c *Client) ListAllChanges(ctx context.Context, pageToken string, pageSize int) ([]*Change, string, error) {
	if pageToken == "" {
		return nil, "", fmt.Errorf("page token is required")
	}

	var allChanges []*Change
	currentPageToken := pageToken
	var newStartPageToken string

	for {
		result, err := c.ListChanges(ctx, currentPageToken, pageSize)
		if err != nil {
			return nil, "", err
		}

		allChanges = append(allChanges, result.Changes...)

		// If we have a newStartPageToken, we've reached the end
		if result.NewStartPageToken != "" {
			newStartPageToken = result.NewStartPageToken
			break
		}

		// If there's a nextPageToken, continue pagination
		if result.NextPageToken != "" {
			currentPageToken = result.NextPageToken
			continue
		}

		// If neither token is present, something is wrong
		return nil, "", fmt.Errorf("no newStartPageToken or nextPageToken in response")
	}

	c.logger.Info("listed all changes",
		zap.Int("total_changes", len(allChanges)),
		zap.String("new_start_page_token", newStartPageToken))

	return allChanges, newStartPageToken, nil
}

// WatchChanges sets up a watch channel for changes.
// This is useful for receiving push notifications instead of polling.
// Note: This feature requires additional setup (webhook endpoint, domain verification).
// For now, we'll focus on polling via ListChanges.
func (c *Client) WatchChanges(ctx context.Context, pageToken string, channelID string, address string) error {
	// TODO: Implement watch channel if needed
	// This requires setting up a webhook endpoint and domain verification
	return fmt.Errorf("watch changes not yet implemented")
}
