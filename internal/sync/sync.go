package sync

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/auth"
	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/driveapi"
	"github.com/sandeepkv93/googlysync/internal/fswatch"
	"github.com/sandeepkv93/googlysync/internal/status"
	"github.com/sandeepkv93/googlysync/internal/storage"
)

// PendingUpload represents a file pending upload to Drive.
type PendingUpload struct {
	OpID      string // Pending operation ID for tracking
	AccountID string
	LocalPath string
	DriveID   string // Empty for new files, set for updates
	ParentID  string
}

// PendingDownload represents a file pending download from Drive.
type PendingDownload struct {
	OpID      string // Pending operation ID for tracking
	AccountID string
	LocalPath string
	Metadata  *driveapi.FileMetadata
}

// Engine coordinates sync operations.
type Engine struct {
	Logger *zap.Logger
	Store  *storage.Storage
	Status *status.Store
	Queue  *Queue
	Auth   *auth.Service
	Config *config.Config

	mu           sync.RWMutex
	driveClients map[string]*driveapi.Client // accountID -> client

	uploadQueue   chan *PendingUpload
	downloadQueue chan *PendingDownload
}

// NewEngine constructs a sync engine.
func NewEngine(
	logger *zap.Logger,
	store *storage.Storage,
	statusStore *status.Store,
	queue *Queue,
	authSvc *auth.Service,
	cfg *config.Config,
) (*Engine, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if store == nil {
		return nil, fmt.Errorf("storage is required")
	}
	if authSvc == nil {
		return nil, fmt.Errorf("auth service is required")
	}
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	logger.Info("sync engine initialized")

	return &Engine{
		Logger:        logger,
		Store:         store,
		Status:        statusStore,
		Queue:         queue,
		Auth:          authSvc,
		Config:        cfg,
		driveClients:  make(map[string]*driveapi.Client),
		uploadQueue:   make(chan *PendingUpload, cfg.SyncQueueSize),
		downloadQueue: make(chan *PendingDownload, cfg.SyncQueueSize),
	}, nil
}

// Run runs the sync loop with upload/download workers and Drive polling.
func (e *Engine) Run(ctx context.Context) {
	// Start worker pools
	go e.uploadWorker(ctx)
	go e.downloadWorker(ctx)

	// Set up polling ticker (every 60 seconds)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	var queueCh <-chan fswatch.Event
	if e.Queue != nil {
		queueCh = e.Queue.Channel()
	}

	e.Logger.Info("sync engine running")

	for {
		select {
		case <-ctx.Done():
			if e.Status != nil {
				e.Status.Update(status.Snapshot{State: status.StateIdle, Message: "idle"})
			}
			e.Logger.Info("sync engine stopped")
			return

		case evt := <-queueCh:
			// Handle local filesystem change
			e.handleLocalChange(ctx, evt)

		case <-ticker.C:
			// Poll Drive Changes API
			e.Logger.Debug("polling drive changes")
			e.pollDriveChanges(ctx)
		}
	}
}

func (e *Engine) handleEvent(evt fswatch.Event) {
	// Deprecated: use handleLocalChange instead
	e.handleLocalChange(context.Background(), evt)
}

// getDriveClient returns a Drive API client for the given account.
// If a client doesn't exist, it creates one using the auth service.
func (e *Engine) getDriveClient(ctx context.Context, accountID string) (*driveapi.Client, error) {
	e.mu.RLock()
	client, exists := e.driveClients[accountID]
	e.mu.RUnlock()

	if exists {
		return client, nil
	}

	// Client doesn't exist, create it
	e.mu.Lock()
	defer e.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := e.driveClients[accountID]; exists {
		return client, nil
	}

	// Get fresh token from auth service
	token, err := e.Auth.RefreshAccessToken(ctx, accountID)
	if err != nil {
		return nil, fmt.Errorf("refresh access token: %w", err)
	}

	// Create new Drive client
	client, err = driveapi.NewClient(ctx, e.Logger, accountID, token)
	if err != nil {
		return nil, fmt.Errorf("create drive client: %w", err)
	}

	// Store client
	e.driveClients[accountID] = client

	e.Logger.Info("created drive client", zap.String("account_id", accountID))

	return client, nil
}

// refreshDriveClient refreshes the token for an existing Drive client.
func (e *Engine) refreshDriveClient(ctx context.Context, accountID string) error {
	// Get fresh token from auth service
	token, err := e.Auth.RefreshAccessToken(ctx, accountID)
	if err != nil {
		return fmt.Errorf("refresh access token: %w", err)
	}

	e.mu.RLock()
	client, exists := e.driveClients[accountID]
	e.mu.RUnlock()

	if !exists {
		return fmt.Errorf("drive client not found for account %s", accountID)
	}

	// Update client with new token
	if err := driveapi.UpdateClient(ctx, client, token); err != nil {
		return fmt.Errorf("update drive client: %w", err)
	}

	e.Logger.Debug("refreshed drive client", zap.String("account_id", accountID))

	return nil
}

// PerformInitialSync performs a full sync of the Drive to local filesystem.
// This should be called once when setting up a new account or after a reset.
func (e *Engine) PerformInitialSync(ctx context.Context, accountID string) error {
	e.Logger.Info("starting initial sync", zap.String("account_id", accountID))

	// Check if initial sync already performed
	syncState, err := e.Store.GetSyncState(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get sync state: %w", err)
	}
	if syncState != nil && syncState.StartPageToken != "" {
		return fmt.Errorf("initial sync already performed for account %s", accountID)
	}

	// Get Drive client
	client, err := e.getDriveClient(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get drive client: %w", err)
	}

	// List all files recursively from root
	e.Logger.Info("listing all files from Drive", zap.String("account_id", accountID))
	files, err := client.ListFilesRecursive(ctx, "root")
	if err != nil {
		return fmt.Errorf("list files recursive: %w", err)
	}

	e.Logger.Info("discovered files",
		zap.String("account_id", accountID),
		zap.Int("count", len(files)))

	// Process files: create local folder structure and queue downloads
	for _, file := range files {
		if file.Trashed {
			continue // Skip trashed files
		}

		if file.MimeType == "application/vnd.google-apps.folder" {
			// Create folder entry
			folder := &storage.Folder{
				AccountID:  accountID,
				DriveID:    file.ID,
				Path:       e.buildLocalPath(accountID, file),
				ModifiedAt: file.ModifiedTime,
				CreatedAt:  time.Now(),
			}
			if err := e.Store.UpsertFolder(ctx, folder); err != nil {
				e.Logger.Warn("failed to upsert folder",
					zap.String("name", file.Name),
					zap.Error(err))
				continue
			}
		} else {
			// Queue file for download
			localPath := e.buildLocalPath(accountID, file)

			// Create pending operation
			opID := uuid.New().String()
			op := &storage.PendingOp{
				ID:        opID,
				AccountID: accountID,
				DriveID:   file.ID,
				OpType:    "download",
				Path:      localPath,
				State:     "pending",
				CreatedAt: time.Now(),
			}
			if err := e.Store.AddPendingOp(ctx, op); err != nil {
				e.Logger.Warn("failed to add pending op",
					zap.String("name", file.Name),
					zap.Error(err))
				continue
			}

			// Add to download queue
			select {
			case e.downloadQueue <- &PendingDownload{
				OpID:      opID,
				AccountID: accountID,
				LocalPath: localPath,
				Metadata:  file,
			}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Get start page token for future incremental syncs
	e.Logger.Info("getting start page token", zap.String("account_id", accountID))
	startPageToken, err := client.GetStartPageToken(ctx)
	if err != nil {
		return fmt.Errorf("get start page token: %w", err)
	}

	// Save sync state
	newSyncState := &storage.SyncState{
		AccountID:      accountID,
		StartPageToken: startPageToken,
		LastSyncAt:     time.Now(),
		Paused:         false,
		UpdatedAt:      time.Now(),
	}
	if err := e.Store.UpsertSyncState(ctx, newSyncState); err != nil {
		return fmt.Errorf("upsert sync state: %w", err)
	}

	e.Logger.Info("initial sync queued",
		zap.String("account_id", accountID),
		zap.Int("files_queued", len(files)),
		zap.String("start_page_token", startPageToken))

	return nil
}

// buildLocalPath constructs the local filesystem path for a Drive file.
// It recursively resolves the parent hierarchy to build the full path.
func (e *Engine) buildLocalPath(accountID string, file *driveapi.FileMetadata) string {
	// TODO: Implement proper path resolution with parent hierarchy
	// For now, use a simple flat structure
	return fmt.Sprintf("%s/%s/%s", e.Config.SyncRoot, accountID, file.Name)
}

// handleLocalChange processes a local filesystem change event.
func (e *Engine) handleLocalChange(ctx context.Context, evt fswatch.Event) {
	// Get active account
	state := e.Auth.State()
	if !state.SignedIn {
		e.Logger.Debug("ignoring event: not signed in", zap.String("path", evt.Path))
		return
	}

	accountID := state.Account.ID

	switch evt.Op {
	case fswatch.OpCreate, fswatch.OpWrite:
		e.queueUpload(ctx, accountID, evt.Path)
	case fswatch.OpRemove:
		e.queueDelete(ctx, accountID, evt.Path)
	default:
		e.Logger.Debug("ignoring event: unsupported operation",
			zap.String("path", evt.Path),
			zap.String("op", fswatch.OpString(evt.Op)))
	}
}

// queueUpload queues a file for upload to Drive.
func (e *Engine) queueUpload(ctx context.Context, accountID, localPath string) {
	e.Logger.Debug("queueing upload",
		zap.String("account_id", accountID),
		zap.String("path", localPath))

	// Check if file exists in storage (to determine if update or create)
	file, err := e.Store.GetFileByPath(ctx, accountID, localPath)
	var driveID, parentID string
	if err == nil && file != nil {
		driveID = file.DriveID
		// TODO: Get parent ID from folder hierarchy
	}

	// Create pending operation
	opID := uuid.New().String()
	op := &storage.PendingOp{
		ID:        opID,
		AccountID: accountID,
		DriveID:   driveID,
		OpType:    "upload",
		Path:      localPath,
		State:     "pending",
		CreatedAt: time.Now(),
	}
	if err := e.Store.AddPendingOp(ctx, op); err != nil {
		e.Logger.Error("failed to add pending upload",
			zap.String("path", localPath),
			zap.Error(err))
		return
	}

	// Send to upload queue
	select {
	case e.uploadQueue <- &PendingUpload{
		OpID:      opID,
		AccountID: accountID,
		LocalPath: localPath,
		DriveID:   driveID,
		ParentID:  parentID,
	}:
		e.Logger.Debug("upload queued", zap.String("path", localPath))
	case <-ctx.Done():
		e.Logger.Debug("upload queue cancelled", zap.String("path", localPath))
	default:
		e.Logger.Warn("upload queue full, dropping event", zap.String("path", localPath))
	}
}

// queueDelete queues a file for deletion from Drive.
func (e *Engine) queueDelete(ctx context.Context, accountID, localPath string) {
	e.Logger.Debug("queueing delete",
		zap.String("account_id", accountID),
		zap.String("path", localPath))

	// Get file from storage to find Drive ID
	file, err := e.Store.GetFileByPath(ctx, accountID, localPath)
	if err != nil {
		e.Logger.Debug("file not found in storage, ignoring delete",
			zap.String("path", localPath),
			zap.Error(err))
		return
	}

	// Create pending operation
	op := &storage.PendingOp{
		AccountID: accountID,
		DriveID:   file.DriveID,
		OpType:    "delete",
		Path:      localPath,
		State:     "pending",
		CreatedAt: time.Now(),
	}
	if err := e.Store.AddPendingOp(ctx, op); err != nil {
		e.Logger.Error("failed to add pending delete",
			zap.String("path", localPath),
			zap.Error(err))
		return
	}

	// TODO: Implement delete worker and queue
	e.Logger.Warn("delete operation not yet implemented", zap.String("path", localPath))
}

// uploadWorker processes uploads from the upload queue.
func (e *Engine) uploadWorker(ctx context.Context) {
	e.Logger.Info("upload worker started")
	defer e.Logger.Info("upload worker stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case upload := <-e.uploadQueue:
			if err := e.processUpload(ctx, upload); err != nil {
				e.Logger.Error("upload failed",
					zap.String("path", upload.LocalPath),
					zap.Error(err))
			}
		}
	}
}

// processUpload uploads a file to Google Drive.
func (e *Engine) processUpload(ctx context.Context, upload *PendingUpload) error {
	e.Logger.Info("processing upload",
		zap.String("account_id", upload.AccountID),
		zap.String("path", upload.LocalPath))

	if e.Status != nil {
		e.Status.Update(status.Snapshot{
			State:   status.StateSyncing,
			Message: fmt.Sprintf("uploading %s", upload.LocalPath),
		})
	}

	// Get Drive client
	client, err := e.getDriveClient(ctx, upload.AccountID)
	if err != nil {
		return fmt.Errorf("get drive client: %w", err)
	}

	// Prepare upload options
	opts := driveapi.UploadOptions{
		FilePath:     upload.LocalPath,
		ParentID:     upload.ParentID,
		UpdateFileID: upload.DriveID,
		OnProgress: func(uploaded, total int64) {
			percent := float64(uploaded) / float64(total) * 100
			if e.Status != nil {
				e.Status.Update(status.Snapshot{
					State:   status.StateSyncing,
					Message: fmt.Sprintf("uploading %s (%.1f%%)", upload.LocalPath, percent),
				})
			}
		},
	}

	// Upload file
	result, err := client.UploadFile(ctx, opts)
	if err != nil {
		// Update pending op with error
		if updateErr := e.Store.UpdatePendingOp(ctx, upload.OpID, "failed", 1, err.Error()); updateErr != nil {
			e.Logger.Error("failed to update pending op",
				zap.String("path", upload.LocalPath),
				zap.Error(updateErr))
		}
		return fmt.Errorf("upload file: %w", err)
	}

	// Save file metadata to storage
	file := &storage.FileRecord{
		AccountID:  upload.AccountID,
		DriveID:    result.File.ID,
		Path:       upload.LocalPath,
		Checksum:   result.File.MD5Checksum,
		Size:       result.File.Size,
		ModifiedAt: result.File.ModifiedTime,
		CreatedAt:  time.Now(),
	}
	if err := e.Store.UpsertFile(ctx, file); err != nil {
		e.Logger.Error("failed to upsert file",
			zap.String("path", upload.LocalPath),
			zap.Error(err))
	}

	// Delete pending operation
	if err := e.Store.DeletePendingOp(ctx, upload.OpID); err != nil {
		e.Logger.Error("failed to delete pending op",
			zap.String("path", upload.LocalPath),
			zap.Error(err))
	}

	e.Logger.Info("upload completed",
		zap.String("account_id", upload.AccountID),
		zap.String("path", upload.LocalPath),
		zap.String("drive_id", result.File.ID))

	if e.Status != nil {
		e.Status.Update(status.Snapshot{
			State:   status.StateIdle,
			Message: "idle",
		})
	}

	return nil
}

// pollDriveChanges polls the Drive Changes API for all accounts.
func (e *Engine) pollDriveChanges(ctx context.Context) {
	accounts, err := e.Store.ListAccounts(ctx)
	if err != nil {
		e.Logger.Error("failed to list accounts", zap.Error(err))
		return
	}

	for _, account := range accounts {
		if err := e.pollAccountChanges(ctx, account.ID); err != nil {
			e.Logger.Error("failed to poll changes",
				zap.String("account_id", account.ID),
				zap.Error(err))
		}
	}
}

// pollAccountChanges polls Drive Changes API for a single account.
func (e *Engine) pollAccountChanges(ctx context.Context, accountID string) error {
	// Get sync state
	syncState, err := e.Store.GetSyncState(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get sync state: %w", err)
	}
	if syncState == nil {
		e.Logger.Debug("no sync state, skipping poll", zap.String("account_id", accountID))
		return nil
	}
	if syncState.Paused {
		e.Logger.Debug("sync paused, skipping poll", zap.String("account_id", accountID))
		return nil
	}

	// Get Drive client
	client, err := e.getDriveClient(ctx, accountID)
	if err != nil {
		return fmt.Errorf("get drive client: %w", err)
	}

	// List all changes since last page token
	changes, newStartPageToken, err := client.ListAllChanges(ctx, syncState.StartPageToken, 100)
	if err != nil {
		return fmt.Errorf("list changes: %w", err)
	}

	e.Logger.Info("polled changes",
		zap.String("account_id", accountID),
		zap.Int("count", len(changes)))

	// Process each change
	for _, change := range changes {
		if err := e.handleRemoteChange(ctx, accountID, change); err != nil {
			e.Logger.Error("failed to handle change",
				zap.String("account_id", accountID),
				zap.String("change_id", change.ChangeID),
				zap.Error(err))
		}
	}

	// Update sync state with new page token
	syncState.StartPageToken = newStartPageToken
	syncState.UpdatedAt = time.Now()
	if err := e.Store.UpsertSyncState(ctx, syncState); err != nil {
		return fmt.Errorf("update sync state: %w", err)
	}

	return nil
}

// handleRemoteChange processes a single change from the Drive Changes API.
func (e *Engine) handleRemoteChange(ctx context.Context, accountID string, change *driveapi.Change) error {
	// Handle removed/trashed files
	if change.Removed || (change.File != nil && change.File.Trashed) {
		return e.handleRemoteDelete(ctx, accountID, change.FileID)
	}

	// Handle folder changes
	if change.File != nil && change.File.MimeType == "application/vnd.google-apps.folder" {
		return e.handleRemoteFolder(ctx, accountID, change.File)
	}

	// Handle file changes (download)
	if change.File != nil {
		return e.queueDownload(ctx, accountID, change.File)
	}

	return nil
}

// handleRemoteDelete handles deletion of a file from Drive.
func (e *Engine) handleRemoteDelete(ctx context.Context, accountID, fileID string) error {
	e.Logger.Debug("handling remote delete",
		zap.String("account_id", accountID),
		zap.String("file_id", fileID))

	// Get file from storage
	file, err := e.Store.GetFileByDriveID(ctx, accountID, fileID)
	if err != nil {
		e.Logger.Debug("file not found in storage, ignoring delete",
			zap.String("file_id", fileID),
			zap.Error(err))
		return nil
	}

	// TODO: Delete local file
	// os.Remove(file.Path)

	// Delete from storage
	if err := e.Store.DeleteFile(ctx, accountID, fileID); err != nil {
		return fmt.Errorf("delete file from storage: %w", err)
	}

	e.Logger.Info("remote delete processed",
		zap.String("account_id", accountID),
		zap.String("file_id", fileID),
		zap.String("path", file.Path))

	return nil
}

// handleRemoteFolder handles creation/update of a folder from Drive.
func (e *Engine) handleRemoteFolder(ctx context.Context, accountID string, file *driveapi.FileMetadata) error {
	e.Logger.Debug("handling remote folder",
		zap.String("account_id", accountID),
		zap.String("folder_id", file.ID),
		zap.String("name", file.Name))

	localPath := e.buildLocalPath(accountID, file)

	// Create folder entry in storage
	folder := &storage.Folder{
		AccountID: accountID,
		DriveID:   file.ID,
		Path:      localPath,
		CreatedAt: time.Now(),
	}
	if err := e.Store.UpsertFolder(ctx, folder); err != nil {
		return fmt.Errorf("upsert folder: %w", err)
	}

	// TODO: Create local directory
	// os.MkdirAll(localPath, 0755)

	return nil
}

// queueDownload queues a file for download from Drive.
func (e *Engine) queueDownload(ctx context.Context, accountID string, file *driveapi.FileMetadata) error {
	localPath := e.buildLocalPath(accountID, file)

	e.Logger.Debug("queueing download",
		zap.String("account_id", accountID),
		zap.String("file_id", file.ID),
		zap.String("path", localPath))

	// Check for conflicts
	hasConflict, err := e.detectConflict(ctx, accountID, localPath, file)
	if err != nil {
		e.Logger.Warn("conflict detection failed",
			zap.String("path", localPath),
			zap.Error(err))
	}
	if hasConflict {
		if err := e.resolveConflict(ctx, accountID, localPath, file); err != nil {
			return fmt.Errorf("resolve conflict: %w", err)
		}
		return nil // Conflict resolution will queue the appropriate operation
	}

	// Create pending operation
	opID := uuid.New().String()
	op := &storage.PendingOp{
		ID:        opID,
		AccountID: accountID,
		DriveID:   file.ID,
		OpType:    "download",
		Path:      localPath,
		State:     "pending",
		CreatedAt: time.Now(),
	}
	if err := e.Store.AddPendingOp(ctx, op); err != nil {
		e.Logger.Error("failed to add pending download",
			zap.String("path", localPath),
			zap.Error(err))
		return fmt.Errorf("add pending op: %w", err)
	}

	// Send to download queue
	select {
	case e.downloadQueue <- &PendingDownload{
		OpID:      opID,
		AccountID: accountID,
		LocalPath: localPath,
		Metadata:  file,
	}:
		e.Logger.Debug("download queued", zap.String("path", localPath))
	case <-ctx.Done():
		return ctx.Err()
	default:
		e.Logger.Warn("download queue full, dropping event", zap.String("path", localPath))
	}

	return nil
}

// downloadWorker processes downloads from the download queue.
func (e *Engine) downloadWorker(ctx context.Context) {
	e.Logger.Info("download worker started")
	defer e.Logger.Info("download worker stopped")

	for {
		select {
		case <-ctx.Done():
			return
		case download := <-e.downloadQueue:
			if err := e.processDownload(ctx, download); err != nil {
				e.Logger.Error("download failed",
					zap.String("path", download.LocalPath),
					zap.Error(err))
			}
		}
	}
}

// processDownload downloads a file from Google Drive.
func (e *Engine) processDownload(ctx context.Context, download *PendingDownload) error {
	e.Logger.Info("processing download",
		zap.String("account_id", download.AccountID),
		zap.String("path", download.LocalPath))

	if e.Status != nil {
		e.Status.Update(status.Snapshot{
			State:   status.StateSyncing,
			Message: fmt.Sprintf("downloading %s", download.LocalPath),
		})
	}

	// Get Drive client
	client, err := e.getDriveClient(ctx, download.AccountID)
	if err != nil {
		return fmt.Errorf("get drive client: %w", err)
	}

	// Get file metadata
	metadata, err := client.GetFile(ctx, download.Metadata.ID)
	if err != nil {
		return fmt.Errorf("get file metadata: %w", err)
	}

	// Prepare download options
	opts := driveapi.DownloadOptions{
		FileID:         metadata.ID,
		DestPath:       download.LocalPath,
		VerifyChecksum: true,
		OnProgress: func(downloaded, total int64) {
			percent := float64(downloaded) / float64(total) * 100
			if e.Status != nil {
				e.Status.Update(status.Snapshot{
					State:   status.StateSyncing,
					Message: fmt.Sprintf("downloading %s (%.1f%%)", download.LocalPath, percent),
				})
			}
		},
	}

	// Download file
	result, err := client.DownloadFile(ctx, opts)
	if err != nil {
		// Update pending op with error
		if updateErr := e.Store.UpdatePendingOp(ctx, download.OpID, "failed", 1, err.Error()); updateErr != nil {
			e.Logger.Error("failed to update pending op",
				zap.String("path", download.LocalPath),
				zap.Error(updateErr))
		}
		return fmt.Errorf("download file: %w", err)
	}

	// Save file metadata to storage
	file := &storage.FileRecord{
		AccountID:  download.AccountID,
		DriveID:    metadata.ID,
		Path:       download.LocalPath,
		Checksum:   metadata.MD5Checksum,
		Size:       metadata.Size,
		ModifiedAt: metadata.ModifiedTime,
		CreatedAt:  time.Now(),
	}
	if err := e.Store.UpsertFile(ctx, file); err != nil {
		e.Logger.Error("failed to upsert file",
			zap.String("path", download.LocalPath),
			zap.Error(err))
	}

	// Delete pending operation
	if err := e.Store.DeletePendingOp(ctx, download.OpID); err != nil {
		e.Logger.Error("failed to delete pending op",
			zap.String("path", download.LocalPath),
			zap.Error(err))
	}

	e.Logger.Info("download completed",
		zap.String("account_id", download.AccountID),
		zap.String("path", download.LocalPath),
		zap.Int64("bytes", result.BytesRead),
		zap.Bool("verified", result.VerifiedOK))

	if e.Status != nil {
		e.Status.Update(status.Snapshot{
			State:   status.StateIdle,
			Message: "idle",
		})
	}

	return nil
}

// detectConflict detects if a file has a conflict between local and remote versions.
// Returns true if there's a conflict that needs resolution.
func (e *Engine) detectConflict(ctx context.Context, accountID, localPath string, remoteMeta *driveapi.FileMetadata) (bool, error) {
	// Check if local file exists
	localInfo, err := os.Stat(localPath)
	if os.IsNotExist(err) {
		// Local file doesn't exist, no conflict
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat local file: %w", err)
	}

	// Get tracked file from storage
	trackedFile, err := e.Store.GetFileByDriveID(ctx, accountID, remoteMeta.ID)
	if err != nil {
		// File not tracked, treat as new download
		return false, nil
	}

	// Compare checksums - if same, no conflict
	if trackedFile.Checksum == remoteMeta.MD5Checksum {
		return false, nil
	}

	// Compare modification times - if equal, no conflict
	if localInfo.ModTime().Equal(remoteMeta.ModifiedTime) {
		return false, nil
	}

	// Compare sizes - if different, likely conflict
	if localInfo.Size() != remoteMeta.Size {
		e.Logger.Warn("conflict detected: size mismatch",
			zap.String("path", localPath),
			zap.Int64("local_size", localInfo.Size()),
			zap.Int64("remote_size", remoteMeta.Size))
		return true, nil
	}

	// Compare modification times - if different, there's a conflict
	if !localInfo.ModTime().Equal(remoteMeta.ModifiedTime) {
		e.Logger.Warn("conflict detected: modification time mismatch",
			zap.String("path", localPath),
			zap.Time("local_time", localInfo.ModTime()),
			zap.Time("remote_time", remoteMeta.ModifiedTime))
		return true, nil
	}

	return false, nil
}

// resolveConflict resolves a conflict using last-write-wins strategy.
// Compares modification times and chooses the newer version.
func (e *Engine) resolveConflict(ctx context.Context, accountID, localPath string, remoteMeta *driveapi.FileMetadata) error {
	localInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}

	if localInfo.ModTime().After(remoteMeta.ModifiedTime) {
		// Local wins - upload
		e.Logger.Info("conflict resolved: local version is newer, uploading",
			zap.String("path", localPath),
			zap.Time("local_time", localInfo.ModTime()),
			zap.Time("remote_time", remoteMeta.ModifiedTime))

		e.queueUpload(ctx, accountID, localPath)
	} else {
		// Remote wins - download
		e.Logger.Info("conflict resolved: remote version is newer, downloading",
			zap.String("path", localPath),
			zap.Time("local_time", localInfo.ModTime()),
			zap.Time("remote_time", remoteMeta.ModifiedTime))

		if err := e.queueDownload(ctx, accountID, remoteMeta); err != nil {
			return fmt.Errorf("queue download: %w", err)
		}
	}

	return nil
}
