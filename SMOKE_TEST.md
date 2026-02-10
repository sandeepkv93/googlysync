# Smoke Test Guide - Google Drive Sync Engine

This guide provides step-by-step instructions for manually testing the sync engine implementation.

## Prerequisites

1. **Google OAuth Credentials**
   - Create a project in [Google Cloud Console](https://console.cloud.google.com)
   - Enable Google Drive API
   - Create OAuth 2.0 credentials (Desktop application)
   - Download credentials and note the Client ID and Client Secret

2. **Configuration**
   ```bash
   export GOOGLYSYNC_OAUTH_CLIENT_ID="your-client-id"
   export GOOGLYSYNC_OAUTH_CLIENT_SECRET="your-client-secret"
   ```

3. **Build the Project**
   ```bash
   task build
   ```

## Test 1: Initial Sync

**Objective:** Verify the daemon can perform a full initial sync from Google Drive.

**Steps:**
1. Start the daemon:
   ```bash
   task run:daemon
   ```

2. In another terminal, trigger initial sync (via IPC or CLI):
   ```bash
   # TBD: Add CLI command when implemented
   ```

3. **Expected Results:**
   - Daemon logs show "initial sync started"
   - All files from Google Drive are downloaded to `~/GoogleDrive/` (or configured sync root)
   - Database populated with file records:
     ```bash
     sqlite3 ~/.local/share/drive-client/googlysync.db "SELECT COUNT(*) FROM files;"
     ```
   - Sync state has `start_page_token` set:
     ```bash
     sqlite3 ~/.local/share/drive-client/googlysync.db "SELECT start_page_token FROM sync_state;"
     ```

**Success Criteria:**
- ✓ No errors in daemon logs
- ✓ All Drive files present locally
- ✓ File checksums match (spot check)
- ✓ Folder hierarchy preserved

## Test 2: Local → Drive Upload

**Objective:** Verify local file changes sync to Google Drive.

**Steps:**
1. Create a test file locally:
   ```bash
   echo "Test content from local" > ~/GoogleDrive/test-upload.txt
   ```

2. Wait 5 seconds for fswatch to detect the change

3. Check daemon logs for upload activity:
   ```bash
   # Look for: "upload queued", "uploading", "upload completed"
   ```

4. Verify file appears in Google Drive web UI

5. Check database:
   ```bash
   sqlite3 ~/.local/share/drive-client/googlysync.db \
     "SELECT * FROM files WHERE path LIKE '%test-upload.txt%';"
   ```

**Success Criteria:**
- ✓ File uploaded within 10 seconds
- ✓ Correct content in Drive
- ✓ Checksum matches local file
- ✓ Database record created

## Test 3: Drive → Local Download

**Objective:** Verify remote file changes sync to local filesystem.

**Steps:**
1. Create a file in Google Drive web UI named `test-download.txt`

2. Wait up to 60 seconds (polling interval) for daemon to detect

3. Check daemon logs for download activity:
   ```bash
   # Look for: "changes detected", "download queued", "download completed"
   ```

4. Verify file exists locally:
   ```bash
   ls -la ~/GoogleDrive/test-download.txt
   ```

5. Check database:
   ```bash
   sqlite3 ~/.local/share/drive-client/googlysync.db \
     "SELECT * FROM files WHERE path LIKE '%test-download.txt%';"
   ```

**Success Criteria:**
- ✓ File downloaded within 70 seconds (60s poll + 10s processing)
- ✓ Correct content locally
- ✓ Checksum matches Drive file
- ✓ Database record created

## Test 4: Conflict Resolution

**Objective:** Verify last-write-wins conflict resolution.

**Steps:**
1. Create a file locally:
   ```bash
   echo "Version 1" > ~/GoogleDrive/conflict-test.txt
   ```

2. Wait for upload to complete

3. Modify the file in both locations simultaneously:
   - Local: `echo "Version 2 - Local" > ~/GoogleDrive/conflict-test.txt`
   - Drive: Edit the file in web UI to say "Version 2 - Remote"

4. Wait for sync cycle (up to 70 seconds)

5. Check daemon logs for conflict detection:
   ```bash
   # Look for: "conflict detected", "conflict resolved: <local|remote> newer"
   ```

6. Verify the newer version won:
   ```bash
   cat ~/GoogleDrive/conflict-test.txt
   # Should show the version with the later modification time
   ```

**Success Criteria:**
- ✓ Conflict detected and logged
- ✓ Newer version (by timestamp) is preserved
- ✓ No data loss
- ✓ Both local and Drive converge to same version

## Test 5: Pending Operations Recovery

**Objective:** Verify pending operations resume after daemon restart.

**Steps:**
1. Create a large file (>100MB) to ensure slow upload:
   ```bash
   dd if=/dev/urandom of=~/GoogleDrive/large-file.bin bs=1M count=100
   ```

2. Immediately stop the daemon (CTRL+C) while upload is in progress

3. Check pending operations:
   ```bash
   sqlite3 ~/.local/share/drive-client/googlysync.db \
     "SELECT * FROM pending_ops WHERE state='pending';"
   ```

4. Restart daemon:
   ```bash
   task run:daemon
   ```

5. Verify upload resumes and completes

**Success Criteria:**
- ✓ Pending operation persists in database
- ✓ Upload resumes on restart
- ✓ File successfully uploaded
- ✓ Pending operation removed after success

## Test 6: Error Handling

**Objective:** Verify graceful error handling and retries.

**Steps:**
1. Disconnect network:
   ```bash
   # macOS: Turn off WiFi
   # Linux: sudo systemctl stop NetworkManager
   ```

2. Create a file locally:
   ```bash
   echo "Test offline" > ~/GoogleDrive/offline-test.txt
   ```

3. Check daemon logs for retry attempts:
   ```bash
   # Look for: "upload failed", "retrying with exponential backoff"
   ```

4. Reconnect network

5. Verify upload eventually succeeds

**Success Criteria:**
- ✓ Upload fails gracefully (no crash)
- ✓ Exponential backoff applied
- ✓ Upload succeeds after network restored
- ✓ Retry count tracked in database

## Test 7: Rate Limiting

**Objective:** Verify rate limiting prevents API quota exhaustion.

**Steps:**
1. Create many small files rapidly:
   ```bash
   for i in {1..100}; do
     echo "File $i" > ~/GoogleDrive/rate-test-$i.txt
   done
   ```

2. Monitor daemon logs for rate limiting:
   ```bash
   # Look for rate limiter delays between API calls
   ```

3. Verify uploads complete without 429 errors

**Success Criteria:**
- ✓ No 429 (Rate Limit Exceeded) errors
- ✓ All files eventually uploaded
- ✓ Rate limiter enforces 10 req/sec limit

## Verification Queries

Useful database queries for verification:

```bash
# Count total files
sqlite3 ~/.local/share/drive-client/googlysync.db \
  "SELECT COUNT(*) FROM files;"

# List pending operations
sqlite3 ~/.local/share/drive-client/googlysync.db \
  "SELECT * FROM pending_ops;"

# Check sync state
sqlite3 ~/.local/share/drive-client/googlysync.db \
  "SELECT * FROM sync_state;"

# List recent files
sqlite3 ~/.local/share/drive-client/googlysync.db \
  "SELECT path, size, modified_at FROM files ORDER BY created_at DESC LIMIT 10;"
```

## Known Limitations

Current implementation has the following known limitations:

1. **Path Resolution:** `buildLocalPath()` uses simple flat structure, needs full parent hierarchy
2. **Local File Creation:** Download pipeline tracks files but doesn't create local files/folders yet
3. **Delete Operations:** Delete operation queuing works but worker not implemented
4. **Google Workspace Files:** Docs/Sheets/Slides export not implemented
5. **Shared Drives:** Only "My Drive" supported, Shared Drives out of scope

## Troubleshooting

### Daemon won't start
- Check OAuth credentials are set
- Verify database path is writable
- Check logs: `tail -f ~/.local/share/drive-client/logs/daemon.jsonl`

### Files not syncing
- Verify daemon is running: `ps aux | grep googlysync`
- Check pending operations table
- Verify network connectivity
- Check Drive API quotas

### Conflicts not resolving
- Check file timestamps are correct
- Verify both files have different modification times
- Check daemon logs for conflict detection messages

## Automated Testing

For automated testing (future work):
```bash
# Run all tests
task test

# Run with race detector
task test:race

# Check coverage
task test:coverage
```

## Success Summary

All smoke tests pass if:
- ✓ Initial sync completes successfully
- ✓ Local → Drive upload works
- ✓ Drive → local download works
- ✓ Conflicts resolve with last-write-wins
- ✓ Pending operations persist and resume
- ✓ Error handling works gracefully
- ✓ Rate limiting prevents quota exhaustion
