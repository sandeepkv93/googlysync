package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Account represents a Google account configured in the client.
type Account struct {
	ID          string
	Email       string
	DisplayName string
	IsPrimary   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TokenRef stores a reference to tokens kept in an external keyring.
type TokenRef struct {
	AccountID string
	KeyID     string
	TokenType string
	Scope     string
	Expiry    time.Time
	UpdatedAt time.Time
}

// SyncState stores account-level sync metadata.
type SyncState struct {
	AccountID      string
	StartPageToken string
	LastSyncAt     time.Time
	LastError      string
	Paused         bool
	UpdatedAt      time.Time
}

// FileRecord represents a Drive file tracked locally.
type FileRecord struct {
	ID         string
	AccountID  string
	Path       string
	DriveID    string
	ETag       string
	Checksum   string
	Size       int64
	ModifiedAt time.Time
	CreatedAt  time.Time
}

// Folder represents a local folder mapping to Drive.
type Folder struct {
	ID         string
	AccountID  string
	Path       string
	DriveID    string
	ParentID   string
	ModifiedAt time.Time
	CreatedAt  time.Time
}

// SharedDrive captures shared drive metadata.
type SharedDrive struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PendingOp tracks deferred sync operations.
type PendingOp struct {
	ID         string
	AccountID  string
	Path       string
	DriveID    string
	OpType     string
	State      string
	RetryCount int
	LastError  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// UpsertAccount creates or updates an account record.
func (s *Storage) UpsertAccount(ctx context.Context, acct *Account) error {
	if acct == nil {
		return nil
	}
	if acct.ID == "" {
		return fmt.Errorf("account id cannot be empty")
	}
	if acct.Email == "" {
		return fmt.Errorf("account email cannot be empty")
	}
	now := time.Now()
	if acct.CreatedAt.IsZero() {
		acct.CreatedAt = now
	}
	if acct.UpdatedAt.IsZero() {
		acct.UpdatedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO accounts (id, email, display_name, is_primary, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			email=excluded.email,
			display_name=excluded.display_name,
			is_primary=excluded.is_primary,
			updated_at=excluded.updated_at
	`, acct.ID, acct.Email, acct.DisplayName, boolToInt(acct.IsPrimary), unixTime(acct.CreatedAt), unixTime(acct.UpdatedAt))
	return err
}

// GetAccount fetches an account by ID.
func (s *Storage) GetAccount(ctx context.Context, id string) (*Account, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, email, display_name, is_primary, created_at, updated_at
		FROM accounts WHERE id = ?
	`, id)
	var acct Account
	var isPrimary int
	var createdAt, updatedAt int64
	if err := row.Scan(&acct.ID, &acct.Email, &acct.DisplayName, &isPrimary, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	acct.IsPrimary = intToBool(isPrimary)
	acct.CreatedAt = fromUnix(createdAt)
	acct.UpdatedAt = fromUnix(updatedAt)
	return &acct, nil
}

// DeleteAccount removes an account (and cascades dependent rows).
func (s *Storage) DeleteAccount(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM accounts WHERE id = ?
	`, id)
	return err
}

// ListAccounts returns all configured accounts.
func (s *Storage) ListAccounts(ctx context.Context) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, email, display_name, is_primary, created_at, updated_at
		FROM accounts ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Account
	for rows.Next() {
		var acct Account
		var isPrimary int
		var createdAt, updatedAt int64
		if err := rows.Scan(&acct.ID, &acct.Email, &acct.DisplayName, &isPrimary, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		acct.IsPrimary = intToBool(isPrimary)
		acct.CreatedAt = fromUnix(createdAt)
		acct.UpdatedAt = fromUnix(updatedAt)
		out = append(out, acct)
	}
	return out, rows.Err()
}

// UpsertTokenRef stores a keyring token reference.
func (s *Storage) UpsertTokenRef(ctx context.Context, ref *TokenRef) error {
	if ref == nil {
		return nil
	}
	if ref.AccountID == "" {
		return fmt.Errorf("token_ref account_id cannot be empty")
	}
	if ref.KeyID == "" {
		return fmt.Errorf("token_ref key_id cannot be empty")
	}
	now := time.Now()
	if ref.UpdatedAt.IsZero() {
		ref.UpdatedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO token_refs (account_id, key_id, token_type, scope, expiry, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			key_id=excluded.key_id,
			token_type=excluded.token_type,
			scope=excluded.scope,
			expiry=excluded.expiry,
			updated_at=excluded.updated_at
	`, ref.AccountID, ref.KeyID, ref.TokenType, ref.Scope, unixTime(ref.Expiry), unixTime(ref.UpdatedAt))
	return err
}

// GetTokenRef returns the token reference for an account.
func (s *Storage) GetTokenRef(ctx context.Context, accountID string) (*TokenRef, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT account_id, key_id, token_type, scope, expiry, updated_at
		FROM token_refs WHERE account_id = ?
	`, accountID)
	var ref TokenRef
	var expiry, updatedAt int64
	if err := row.Scan(&ref.AccountID, &ref.KeyID, &ref.TokenType, &ref.Scope, &expiry, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ref.Expiry = fromUnix(expiry)
	ref.UpdatedAt = fromUnix(updatedAt)
	return &ref, nil
}

// DeleteTokenRef removes a token reference for an account.
func (s *Storage) DeleteTokenRef(ctx context.Context, accountID string) error {
	if accountID == "" {
		return fmt.Errorf("token_ref account_id cannot be empty")
	}
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM token_refs WHERE account_id = ?
	`, accountID)
	return err
}

// UpsertSyncState stores account sync metadata.
func (s *Storage) UpsertSyncState(ctx context.Context, state *SyncState) error {
	if state == nil {
		return nil
	}
	if state.AccountID == "" {
		return fmt.Errorf("sync_state account_id cannot be empty")
	}
	now := time.Now()
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO sync_state (account_id, start_page_token, last_sync_at, last_error, paused, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(account_id) DO UPDATE SET
			start_page_token=excluded.start_page_token,
			last_sync_at=excluded.last_sync_at,
			last_error=excluded.last_error,
			paused=excluded.paused,
			updated_at=excluded.updated_at
	`, state.AccountID, state.StartPageToken, unixTime(state.LastSyncAt), state.LastError, boolToInt(state.Paused), unixTime(state.UpdatedAt))
	return err
}

// GetSyncState returns the sync metadata for an account.
func (s *Storage) GetSyncState(ctx context.Context, accountID string) (*SyncState, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT account_id, start_page_token, last_sync_at, last_error, paused, updated_at
		FROM sync_state WHERE account_id = ?
	`, accountID)
	var state SyncState
	var lastSyncAt, updatedAt int64
	var paused int
	if err := row.Scan(&state.AccountID, &state.StartPageToken, &lastSyncAt, &state.LastError, &paused, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	state.LastSyncAt = fromUnix(lastSyncAt)
	state.Paused = intToBool(paused)
	state.UpdatedAt = fromUnix(updatedAt)
	return &state, nil
}

// UpsertFile creates or updates a file record.
func (s *Storage) UpsertFile(ctx context.Context, file *FileRecord) error {
	if file == nil {
		return nil
	}
	if file.ID == "" {
		return fmt.Errorf("file id cannot be empty")
	}
	if file.AccountID == "" {
		return fmt.Errorf("file account_id cannot be empty")
	}
	if file.Path == "" {
		return fmt.Errorf("file path cannot be empty")
	}
	if file.DriveID == "" {
		return fmt.Errorf("file drive_id cannot be empty")
	}
	now := time.Now()
	if file.CreatedAt.IsZero() {
		file.CreatedAt = now
	}
	if file.ModifiedAt.IsZero() {
		file.ModifiedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO files (id, account_id, path, drive_id, etag, checksum, size, modified_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id=excluded.account_id,
			path=excluded.path,
			drive_id=excluded.drive_id,
			etag=excluded.etag,
			checksum=excluded.checksum,
			size=excluded.size,
			modified_at=excluded.modified_at
	`, file.ID, file.AccountID, file.Path, file.DriveID, file.ETag, file.Checksum, file.Size, unixTime(file.ModifiedAt), unixTime(file.CreatedAt))
	return err
}

// GetFileByPath returns a file record by account and path.
func (s *Storage) GetFileByPath(ctx context.Context, accountID, path string) (*FileRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, account_id, path, drive_id, etag, checksum, size, modified_at, created_at
		FROM files WHERE account_id = ? AND path = ?
	`, accountID, path)
	var file FileRecord
	var modifiedAt, createdAt int64
	if err := row.Scan(&file.ID, &file.AccountID, &file.Path, &file.DriveID, &file.ETag, &file.Checksum, &file.Size, &modifiedAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	file.ModifiedAt = fromUnix(modifiedAt)
	file.CreatedAt = fromUnix(createdAt)
	return &file, nil
}

// GetFileByDriveID returns a file record by account and Drive ID.
func (s *Storage) GetFileByDriveID(ctx context.Context, accountID, driveID string) (*FileRecord, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, account_id, path, drive_id, etag, checksum, size, modified_at, created_at
		FROM files WHERE account_id = ? AND drive_id = ?
	`, accountID, driveID)
	var file FileRecord
	var modifiedAt, createdAt int64
	if err := row.Scan(&file.ID, &file.AccountID, &file.Path, &file.DriveID, &file.ETag, &file.Checksum, &file.Size, &modifiedAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	file.ModifiedAt = fromUnix(modifiedAt)
	file.CreatedAt = fromUnix(createdAt)
	return &file, nil
}

// DeleteFile removes a file record by account and path.
func (s *Storage) DeleteFile(ctx context.Context, accountID, path string) error {
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM files WHERE account_id = ? AND path = ?
	`, accountID, path)
	return err
}

// ListFilesByPrefix returns files under a path prefix.
func (s *Storage) ListFilesByPrefix(ctx context.Context, accountID, prefix string, limit int) ([]FileRecord, error) {
	if limit <= 0 {
		limit = 500
	}
	pattern := escapeLike(prefix) + "%"
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, account_id, path, drive_id, etag, checksum, size, modified_at, created_at
		FROM files
		WHERE account_id = ? AND path LIKE ? ESCAPE '\'
		ORDER BY path ASC
		LIMIT ?
	`, accountID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FileRecord
	for rows.Next() {
		var file FileRecord
		var modifiedAt, createdAt int64
		if err := rows.Scan(&file.ID, &file.AccountID, &file.Path, &file.DriveID, &file.ETag, &file.Checksum, &file.Size, &modifiedAt, &createdAt); err != nil {
			return nil, err
		}
		file.ModifiedAt = fromUnix(modifiedAt)
		file.CreatedAt = fromUnix(createdAt)
		out = append(out, file)
	}
	return out, rows.Err()
}

// UpsertFolder stores a folder record.
func (s *Storage) UpsertFolder(ctx context.Context, folder *Folder) error {
	if folder == nil {
		return nil
	}
	if folder.ID == "" {
		return fmt.Errorf("folder id cannot be empty")
	}
	if folder.AccountID == "" {
		return fmt.Errorf("folder account_id cannot be empty")
	}
	if folder.Path == "" {
		return fmt.Errorf("folder path cannot be empty")
	}
	if folder.DriveID == "" {
		return fmt.Errorf("folder drive_id cannot be empty")
	}
	now := time.Now()
	if folder.CreatedAt.IsZero() {
		folder.CreatedAt = now
	}
	if folder.ModifiedAt.IsZero() {
		folder.ModifiedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO folders (id, account_id, path, drive_id, parent_id, modified_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			account_id=excluded.account_id,
			path=excluded.path,
			drive_id=excluded.drive_id,
			parent_id=excluded.parent_id,
			modified_at=excluded.modified_at
	`, folder.ID, folder.AccountID, folder.Path, folder.DriveID, folder.ParentID, unixTime(folder.ModifiedAt), unixTime(folder.CreatedAt))
	return err
}

// ListFoldersByPrefix returns folders under a path prefix.
func (s *Storage) ListFoldersByPrefix(ctx context.Context, accountID, prefix string, limit int) ([]Folder, error) {
	if limit <= 0 {
		limit = 500
	}
	pattern := escapeLike(prefix) + "%"
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, account_id, path, drive_id, parent_id, modified_at, created_at
		FROM folders
		WHERE account_id = ? AND path LIKE ? ESCAPE '\'
		ORDER BY path ASC
		LIMIT ?
	`, accountID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Folder
	for rows.Next() {
		var folder Folder
		var modifiedAt, createdAt int64
		if err := rows.Scan(&folder.ID, &folder.AccountID, &folder.Path, &folder.DriveID, &folder.ParentID, &modifiedAt, &createdAt); err != nil {
			return nil, err
		}
		folder.ModifiedAt = fromUnix(modifiedAt)
		folder.CreatedAt = fromUnix(createdAt)
		out = append(out, folder)
	}
	return out, rows.Err()
}

// UpsertSharedDrive stores shared drive metadata.
func (s *Storage) UpsertSharedDrive(ctx context.Context, drive *SharedDrive) error {
	if drive == nil {
		return nil
	}
	if drive.ID == "" {
		return fmt.Errorf("shared_drive id cannot be empty")
	}
	if drive.Name == "" {
		return fmt.Errorf("shared_drive name cannot be empty")
	}
	now := time.Now()
	if drive.CreatedAt.IsZero() {
		drive.CreatedAt = now
	}
	if drive.UpdatedAt.IsZero() {
		drive.UpdatedAt = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO shared_drives (id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			updated_at=excluded.updated_at
	`, drive.ID, drive.Name, unixTime(drive.CreatedAt), unixTime(drive.UpdatedAt))
	return err
}

// ListSharedDrives returns all shared drives.
func (s *Storage) ListSharedDrives(ctx context.Context) ([]SharedDrive, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, name, created_at, updated_at
		FROM shared_drives
		ORDER BY name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SharedDrive
	for rows.Next() {
		var drive SharedDrive
		var createdAt, updatedAt int64
		if err := rows.Scan(&drive.ID, &drive.Name, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		drive.CreatedAt = fromUnix(createdAt)
		drive.UpdatedAt = fromUnix(updatedAt)
		out = append(out, drive)
	}
	return out, rows.Err()
}

// AddPendingOp inserts a new pending operation.
func (s *Storage) AddPendingOp(ctx context.Context, op *PendingOp) error {
	if op == nil {
		return nil
	}
	if op.ID == "" {
		return fmt.Errorf("pending_op id cannot be empty")
	}
	if op.AccountID == "" {
		return fmt.Errorf("pending_op account_id cannot be empty")
	}
	if op.Path == "" {
		return fmt.Errorf("pending_op path cannot be empty")
	}
	if op.OpType == "" {
		return fmt.Errorf("pending_op op_type cannot be empty")
	}
	now := time.Now()
	if op.CreatedAt.IsZero() {
		op.CreatedAt = now
	}
	if op.UpdatedAt.IsZero() {
		op.UpdatedAt = now
	}
	if op.State == "" {
		op.State = "queued"
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO pending_ops (id, account_id, path, drive_id, op_type, state, retry_count, last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, op.ID, op.AccountID, op.Path, op.DriveID, op.OpType, op.State, op.RetryCount, op.LastError, unixTime(op.CreatedAt), unixTime(op.UpdatedAt))
	return err
}

// ListPendingOps returns pending ops for an account, optionally filtered by state.
func (s *Storage) ListPendingOps(ctx context.Context, accountID, state string, limit int) ([]PendingOp, error) {
	if limit <= 0 {
		limit = 500
	}
	query := `
		SELECT id, account_id, path, drive_id, op_type, state, retry_count, last_error, created_at, updated_at
		FROM pending_ops
		WHERE account_id = ?
	`
	var args []any
	args = append(args, accountID)
	if state != "" {
		query += " AND state = ?"
		args = append(args, state)
	}
	query += " ORDER BY created_at ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingOp
	for rows.Next() {
		var op PendingOp
		var createdAt, updatedAt int64
		if err := rows.Scan(&op.ID, &op.AccountID, &op.Path, &op.DriveID, &op.OpType, &op.State, &op.RetryCount, &op.LastError, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		op.CreatedAt = fromUnix(createdAt)
		op.UpdatedAt = fromUnix(updatedAt)
		out = append(out, op)
	}
	return out, rows.Err()
}

// UpdatePendingOp updates pending op state and metadata.
func (s *Storage) UpdatePendingOp(ctx context.Context, id, state string, retryCount int, lastError string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE pending_ops
		SET state = ?, retry_count = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`, state, retryCount, lastError, unixTime(time.Now()), id)
	return err
}

// DeletePendingOp removes a pending op.
func (s *Storage) DeletePendingOp(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM pending_ops WHERE id = ?
	`, id)
	return err
}

func unixTime(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func fromUnix(sec int64) time.Time {
	if sec == 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}

func intToBool(val int) bool {
	return val != 0
}

func escapeLike(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"%", "\\%",
		"_", "\\_",
	)
	return replacer.Replace(value)
}
