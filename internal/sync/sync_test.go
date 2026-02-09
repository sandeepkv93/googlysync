package sync

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/auth"
	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/driveapi"
	"github.com/sandeepkv93/googlysync/internal/status"
	"github.com/sandeepkv93/googlysync/internal/storage"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()

	// Create test storage
	dir := t.TempDir()
	cfg := &config.Config{
		DatabasePath: filepath.Join(dir, "test.db"),
		SyncRoot:     filepath.Join(dir, "sync"),
		SyncQueueSize: 10,
	}

	store, err := storage.NewStorage(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Create test auth service
	authSvc, err := auth.NewService(context.Background(), zap.NewNop(), cfg, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	// Create status store
	statusStore := status.NewStore()

	// Create queue
	queue := NewQueue(zap.NewNop(), 10)

	// Create engine
	engine, err := NewEngine(zap.NewNop(), store, statusStore, queue, authSvc, cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	return engine
}

func TestNewEngine(t *testing.T) {
	tests := []struct {
		name      string
		logger    *zap.Logger
		store     *storage.Storage
		status    *status.Store
		queue     *Queue
		auth      *auth.Service
		cfg       *config.Config
		wantErr   bool
	}{
		{
			name:    "missing logger",
			logger:  nil,
			wantErr: true,
		},
		{
			name:    "missing storage",
			logger:  zap.NewNop(),
			store:   nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEngine(tt.logger, tt.store, tt.status, tt.queue, tt.auth, tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEngine() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewEngineSuccess(t *testing.T) {
	engine := newTestEngine(t)

	if engine.Logger == nil {
		t.Error("Engine.Logger is nil")
	}
	if engine.Store == nil {
		t.Error("Engine.Store is nil")
	}
	if engine.Auth == nil {
		t.Error("Engine.Auth is nil")
	}
	if engine.Config == nil {
		t.Error("Engine.Config is nil")
	}
	if engine.driveClients == nil {
		t.Error("Engine.driveClients is nil")
	}
	if engine.uploadQueue == nil {
		t.Error("Engine.uploadQueue is nil")
	}
	if engine.downloadQueue == nil {
		t.Error("Engine.downloadQueue is nil")
	}
}

func TestBuildLocalPath(t *testing.T) {
	engine := newTestEngine(t)

	file := &driveapi.FileMetadata{
		ID:   "file-123",
		Name: "test.txt",
	}

	path := engine.buildLocalPath("account-1", file)
	if path == "" {
		t.Error("buildLocalPath() returned empty string")
	}

	// Should contain account ID and file name
	if filepath.Base(path) != "test.txt" {
		t.Errorf("buildLocalPath() filename = %v, want test.txt", filepath.Base(path))
	}
}

func TestDetectConflict(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// Test with non-existent local file - no conflict
	remoteMeta := &driveapi.FileMetadata{
		ID:           "file-123",
		Name:         "test.txt",
		Size:         100,
		MD5Checksum:  "abc123",
		ModifiedTime: time.Now(),
	}

	hasConflict, err := engine.detectConflict(ctx, "account-1", "/nonexistent/file.txt", remoteMeta)
	if err != nil {
		t.Errorf("detectConflict() error = %v, want nil", err)
	}
	if hasConflict {
		t.Error("detectConflict() = true, want false for non-existent file")
	}
}

func TestQueueSizes(t *testing.T) {
	engine := newTestEngine(t)

	// Check queue capacities
	uploadCap := cap(engine.uploadQueue)
	downloadCap := cap(engine.downloadQueue)

	if uploadCap != engine.Config.SyncQueueSize {
		t.Errorf("uploadQueue capacity = %d, want %d", uploadCap, engine.Config.SyncQueueSize)
	}
	if downloadCap != engine.Config.SyncQueueSize {
		t.Errorf("downloadQueue capacity = %d, want %d", downloadCap, engine.Config.SyncQueueSize)
	}
}

func TestPendingUploadStruct(t *testing.T) {
	upload := &PendingUpload{
		OpID:      "op-123",
		AccountID: "account-1",
		LocalPath: "/path/to/file.txt",
		DriveID:   "drive-123",
		ParentID:  "parent-123",
	}

	if upload.OpID != "op-123" {
		t.Errorf("PendingUpload.OpID = %v, want op-123", upload.OpID)
	}
	if upload.AccountID != "account-1" {
		t.Errorf("PendingUpload.AccountID = %v, want account-1", upload.AccountID)
	}
}

func TestPendingDownloadStruct(t *testing.T) {
	metadata := &driveapi.FileMetadata{
		ID:   "file-123",
		Name: "test.txt",
	}

	download := &PendingDownload{
		OpID:      "op-456",
		AccountID: "account-1",
		LocalPath: "/path/to/file.txt",
		Metadata:  metadata,
	}

	if download.OpID != "op-456" {
		t.Errorf("PendingDownload.OpID = %v, want op-456", download.OpID)
	}
	if download.Metadata == nil {
		t.Error("PendingDownload.Metadata is nil")
	}
	if download.Metadata.ID != "file-123" {
		t.Errorf("PendingDownload.Metadata.ID = %v, want file-123", download.Metadata.ID)
	}
}

func TestGetDriveClientNotFound(t *testing.T) {
	engine := newTestEngine(t)
	ctx := context.Background()

	// Should fail because no account is signed in
	_, err := engine.getDriveClient(ctx, "nonexistent-account")
	if err == nil {
		t.Error("getDriveClient() error = nil, want error for nonexistent account")
	}
}

func TestEngineStructFields(t *testing.T) {
	engine := newTestEngine(t)

	// Verify all required fields are initialized
	fields := map[string]interface{}{
		"Logger":        engine.Logger,
		"Store":         engine.Store,
		"Status":        engine.Status,
		"Queue":         engine.Queue,
		"Auth":          engine.Auth,
		"Config":        engine.Config,
		"driveClients":  engine.driveClients,
		"uploadQueue":   engine.uploadQueue,
		"downloadQueue": engine.downloadQueue,
	}

	for name, field := range fields {
		if field == nil {
			t.Errorf("Engine.%s is nil", name)
		}
	}
}
