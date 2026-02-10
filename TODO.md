# Implementation TODOs

This document tracks remaining implementation work for the sync engine.

## High Priority (Blocks Basic Functionality)

### 1. Implement Path Resolution with Parent Hierarchy
**File:** `internal/sync/sync.go:318`
**Current:** Simple flat structure `syncRoot/accountID/filename`
**Needed:** Full path resolution with parent hierarchy traversal

```go
// Current implementation:
func (e *Engine) buildLocalPath(accountID string, file *driveapi.FileMetadata) string {
    return fmt.Sprintf("%s/%s/%s", e.Config.SyncRoot, accountID, file.Name)
}

// Should implement:
// 1. Resolve parent folder IDs recursively
// 2. Build full path: syncRoot/accountID/folder1/folder2/.../filename
// 3. Handle multiple files with same name in different folders
// 4. Cache folder paths for performance
```

**Impact:** Without this, all files are placed in root directory, losing folder structure.

### 2. Create Local Directories During Download
**File:** `internal/sync/sync.go:674`
**Current:** Commented out `os.MkdirAll(localPath, 0755)`
**Needed:** Create parent directories before downloading files

```go
// In handleRemoteFolder():
if err := os.MkdirAll(localPath, 0755); err != nil {
    return fmt.Errorf("create directory: %w", err)
}
```

**Impact:** Downloads will fail if parent directories don't exist.

### 3. Create Local Files During Download
**File:** `internal/sync/sync.go:638`
**Current:** Commented out `os.Remove(file.Path)`
**Needed:** Actually write downloaded files to disk

**Impact:** Files are tracked in database but not created locally.

### 4. Get Parent ID from Folder Hierarchy
**File:** `internal/sync/sync.go:357`
**Current:** parentID is empty string
**Needed:** Look up parent folder ID from storage based on local path

```go
// Pseudo-code:
parentPath := filepath.Dir(localPath)
parentFolder, err := e.Store.GetFolderByPath(ctx, accountID, parentPath)
if err == nil && parentFolder != nil {
    parentID = parentFolder.DriveID
}
```

**Impact:** Uploaded files won't be placed in correct Drive folders.

## Medium Priority (Enhances Functionality)

### 5. Implement Delete Worker and Queue
**File:** `internal/sync/sync.go:426`
**Status:** Delete operations are queued but not processed
**Needed:**
- Create `deleteWorker()` goroutine
- Create `deleteQueue` channel
- Process delete operations from queue
- Call Drive API to trash/delete files

**Impact:** Local file deletions don't sync to Drive.

### 6. Implement Google Workspace File Export
**File:** `internal/driveapi/downloads.go:258`
**Status:** Returns error for Docs/Sheets/Slides
**Needed:**
- Detect Google Workspace MIME types
- Map to export formats (Docs→PDF/DOCX, Sheets→XLSX, etc.)
- Use `Files.Export()` API instead of `Download()`
- Handle export format preferences

**Impact:** Google Workspace files can't be synced, only native files.

### 7. Implement Change Watch Channel
**File:** `internal/driveapi/changes.go:167`
**Status:** Returns "not yet implemented" error
**Needed:**
- Set up HTTPS webhook endpoint
- Verify domain ownership
- Create watch channel with `Changes.Watch()`
- Handle push notifications instead of polling

**Impact:** Currently polls every 60 seconds; push notifications would be instant.

## Low Priority (Nice to Have)

### 8. Sophisticated Network Error Detection
**File:** `internal/driveapi/errors.go:56`
**Status:** Basic retry logic works
**Needed:**
- Detect DNS errors
- Detect connection timeout vs read timeout
- Distinguish temporary vs permanent network failures
- Add context deadline exceeded handling

**Impact:** Minor - current retry logic covers most cases.

## Implementation Order Recommendation

1. **Phase 1 (Critical Path):**
   - Path resolution with parent hierarchy (#1)
   - Get parent ID from folder hierarchy (#4)
   - Create local directories (#2)
   - Create local files (#3)

2. **Phase 2 (Complete Basic Sync):**
   - Implement delete worker (#5)

3. **Phase 3 (Feature Complete):**
   - Google Workspace export (#6)
   - Watch channel (#7)

4. **Phase 4 (Polish):**
   - Network error improvements (#8)

## Additional Work Not in TODOs

### Testing
- Mock Drive API for comprehensive unit tests
- Integration tests with test Drive account
- Performance tests with large file sets

### Error Handling
- Retry queue processing for failed operations
- Circuit breaker for API failures
- Better error messages for users

### Features
- Shared Drive support
- Selective sync (choose folders)
- Bandwidth throttling
- Pause/resume sync

### DevOps
- Metrics and monitoring
- Structured logging improvements
- Configuration validation
- Health check endpoint

## Progress Tracking

| Item | Priority | Status | Effort |
|------|----------|--------|--------|
| #1 Path Resolution | High | 🔴 Todo | 4h |
| #2 Create Directories | High | 🔴 Todo | 1h |
| #3 Create Files | High | 🔴 Todo | 1h |
| #4 Parent ID Lookup | High | 🔴 Todo | 2h |
| #5 Delete Worker | Medium | 🔴 Todo | 3h |
| #6 Workspace Export | Medium | 🔴 Todo | 6h |
| #7 Watch Channel | Medium | 🔴 Todo | 8h |
| #8 Network Errors | Low | 🔴 Todo | 2h |

**Total Estimated Effort:** ~27 hours for all TODOs
**Critical Path:** ~8 hours for basic functionality

## Notes

- All TODOs have clear placeholders in code with comments
- Most TODOs are straightforward implementations
- Path resolution (#1) is the most complex
- Watch channel (#7) requires significant infrastructure setup
