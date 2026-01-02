package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/config"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		DatabasePath: filepath.Join(dir, "googlysync.db"),
	}
	store, err := NewStorage(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestAccountAndTokenRef(t *testing.T) {
	store := newTestStorage(t)
	ctx := context.Background()

	createdAt := time.Unix(1_700_000_000, 0)
	updatedAt := time.Unix(1_700_000_100, 0)
	acct := &Account{
		ID:          "acct-1",
		Email:       "user@example.com",
		DisplayName: "User",
		IsPrimary:   true,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
	if err := store.UpsertAccount(ctx, acct); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	got, err := store.GetAccount(ctx, "acct-1")
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if got == nil || got.Email != acct.Email || got.DisplayName != acct.DisplayName || got.IsPrimary != acct.IsPrimary {
		t.Fatalf("GetAccount mismatch: %#v", got)
	}
	if !got.CreatedAt.Equal(createdAt) || !got.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("GetAccount times mismatch: %#v", got)
	}

	list, err := store.ListAccounts(ctx)
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(list) != 1 || list[0].ID != acct.ID {
		t.Fatalf("ListAccounts mismatch: %#v", list)
	}

	expiry := time.Unix(1_700_001_000, 0)
	ref := &TokenRef{
		AccountID: "acct-1",
		KeyID:     "key-1",
		TokenType: "bearer",
		Scope:     "drive",
		Expiry:    expiry,
		UpdatedAt: updatedAt,
	}
	if err := store.UpsertTokenRef(ctx, ref); err != nil {
		t.Fatalf("UpsertTokenRef: %v", err)
	}
	gotRef, err := store.GetTokenRef(ctx, "acct-1")
	if err != nil {
		t.Fatalf("GetTokenRef: %v", err)
	}
	if gotRef == nil || gotRef.KeyID != ref.KeyID || gotRef.TokenType != ref.TokenType || gotRef.Scope != ref.Scope {
		t.Fatalf("GetTokenRef mismatch: %#v", gotRef)
	}
	if !gotRef.Expiry.Equal(expiry) {
		t.Fatalf("GetTokenRef expiry mismatch: %#v", gotRef)
	}
}

func TestSyncState(t *testing.T) {
	store := newTestStorage(t)
	ctx := context.Background()

	if err := store.UpsertAccount(ctx, &Account{ID: "acct-1", Email: "user@example.com"}); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	lastSync := time.Unix(1_700_002_000, 0)
	state := &SyncState{
		AccountID:      "acct-1",
		StartPageToken: "token-1",
		LastSyncAt:     lastSync,
		LastError:      "",
		Paused:         false,
		UpdatedAt:      lastSync,
	}
	if err := store.UpsertSyncState(ctx, state); err != nil {
		t.Fatalf("UpsertSyncState: %v", err)
	}

	got, err := store.GetSyncState(ctx, "acct-1")
	if err != nil {
		t.Fatalf("GetSyncState: %v", err)
	}
	if got == nil || got.StartPageToken != state.StartPageToken || got.Paused != state.Paused {
		t.Fatalf("GetSyncState mismatch: %#v", got)
	}
	if !got.LastSyncAt.Equal(lastSync) {
		t.Fatalf("GetSyncState time mismatch: %#v", got)
	}
}

func TestFilesAndFolders(t *testing.T) {
	store := newTestStorage(t)
	ctx := context.Background()

	if err := store.UpsertAccount(ctx, &Account{ID: "acct-1", Email: "user@example.com"}); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	modifiedAt := time.Unix(1_700_003_000, 0)
	file := &FileRecord{
		ID:         "file-1",
		AccountID:  "acct-1",
		Path:       "docs/report.txt",
		DriveID:    "drive-1",
		ETag:       "etag-1",
		Checksum:   "chk-1",
		Size:       128,
		ModifiedAt: modifiedAt,
		CreatedAt:  modifiedAt,
	}
	if err := store.UpsertFile(ctx, file); err != nil {
		t.Fatalf("UpsertFile: %v", err)
	}

	got, err := store.GetFileByPath(ctx, "acct-1", "docs/report.txt")
	if err != nil {
		t.Fatalf("GetFileByPath: %v", err)
	}
	if got == nil || got.ID != file.ID || got.DriveID != file.DriveID {
		t.Fatalf("GetFileByPath mismatch: %#v", got)
	}
	if !got.ModifiedAt.Equal(modifiedAt) {
		t.Fatalf("GetFileByPath time mismatch: %#v", got)
	}

	list, err := store.ListFilesByPrefix(ctx, "acct-1", "docs/", 0)
	if err != nil {
		t.Fatalf("ListFilesByPrefix: %v", err)
	}
	if len(list) != 1 || list[0].ID != file.ID {
		t.Fatalf("ListFilesByPrefix mismatch: %#v", list)
	}

	if err := store.DeleteFile(ctx, "acct-1", "docs/report.txt"); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	empty, err := store.GetFileByPath(ctx, "acct-1", "docs/report.txt")
	if err != nil {
		t.Fatalf("GetFileByPath after delete: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected file deleted, got %#v", empty)
	}

	folder := &Folder{
		ID:         "folder-1",
		AccountID:  "acct-1",
		Path:       "docs",
		DriveID:    "drive-folder-1",
		ParentID:   "root",
		CreatedAt:  modifiedAt,
		ModifiedAt: modifiedAt,
	}
	if err := store.UpsertFolder(ctx, folder); err != nil {
		t.Fatalf("UpsertFolder: %v", err)
	}
	folders, err := store.ListFoldersByPrefix(ctx, "acct-1", "docs", 0)
	if err != nil {
		t.Fatalf("ListFoldersByPrefix: %v", err)
	}
	if len(folders) != 1 || folders[0].ID != folder.ID {
		t.Fatalf("ListFoldersByPrefix mismatch: %#v", folders)
	}
}

func TestPendingOps(t *testing.T) {
	store := newTestStorage(t)
	ctx := context.Background()

	if err := store.UpsertAccount(ctx, &Account{ID: "acct-1", Email: "user@example.com"}); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	op := &PendingOp{
		ID:        "op-1",
		AccountID: "acct-1",
		Path:      "docs/report.txt",
		DriveID:   "drive-1",
		OpType:    "upload",
	}
	if err := store.AddPendingOp(ctx, op); err != nil {
		t.Fatalf("AddPendingOp: %v", err)
	}

	list, err := store.ListPendingOps(ctx, "acct-1", "queued", 0)
	if err != nil {
		t.Fatalf("ListPendingOps: %v", err)
	}
	if len(list) != 1 || list[0].ID != op.ID {
		t.Fatalf("ListPendingOps mismatch: %#v", list)
	}

	if err := store.UpdatePendingOp(ctx, "op-1", "done", 1, ""); err != nil {
		t.Fatalf("UpdatePendingOp: %v", err)
	}
	done, err := store.ListPendingOps(ctx, "acct-1", "done", 0)
	if err != nil {
		t.Fatalf("ListPendingOps done: %v", err)
	}
	if len(done) != 1 || done[0].State != "done" {
		t.Fatalf("ListPendingOps done mismatch: %#v", done)
	}

	if err := store.DeletePendingOp(ctx, "op-1"); err != nil {
		t.Fatalf("DeletePendingOp: %v", err)
	}
	empty, err := store.ListPendingOps(ctx, "acct-1", "", 0)
	if err != nil {
		t.Fatalf("ListPendingOps after delete: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected no pending ops, got %#v", empty)
	}
}

func TestSharedDrives(t *testing.T) {
	store := newTestStorage(t)
	ctx := context.Background()

	drive := &SharedDrive{
		ID:        "drive-1",
		Name:      "Team Drive",
		CreatedAt: time.Unix(1_700_004_000, 0),
		UpdatedAt: time.Unix(1_700_004_100, 0),
	}
	if err := store.UpsertSharedDrive(ctx, drive); err != nil {
		t.Fatalf("UpsertSharedDrive: %v", err)
	}
	list, err := store.ListSharedDrives(ctx)
	if err != nil {
		t.Fatalf("ListSharedDrives: %v", err)
	}
	if len(list) != 1 || list[0].ID != drive.ID {
		t.Fatalf("ListSharedDrives mismatch: %#v", list)
	}
}
