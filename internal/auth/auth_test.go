package auth

import (
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/sandeepkv93/googlysync/internal/config"
	"github.com/sandeepkv93/googlysync/internal/storage"
)

func newTestStore(t *testing.T) *storage.Storage {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{DatabasePath: filepath.Join(dir, "auth.db")}
	store, err := storage.NewStorage(cfg, zap.NewNop())
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return store
}

func TestNewServiceBootstrapsState(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	account := storage.Account{
		ID:          "acct-1",
		Email:       "user@example.com",
		DisplayName: "User",
		IsPrimary:   true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := store.UpsertAccount(ctx, &account); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}
	ref := storage.TokenRef{
		AccountID: account.ID,
		KeyID:     account.ID,
		TokenType: "refresh",
		Scope:     "scope",
		Expiry:    time.Now().Add(time.Hour),
		UpdatedAt: time.Now(),
	}
	if err := store.UpsertTokenRef(ctx, &ref); err != nil {
		t.Fatalf("UpsertTokenRef: %v", err)
	}

	svc, err := NewService(ctx, zap.NewNop(), &config.Config{}, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	state := svc.State()
	if !state.SignedIn {
		t.Fatalf("expected SignedIn true")
	}
	if state.Account.ID != account.ID {
		t.Fatalf("expected account %q, got %q", account.ID, state.Account.ID)
	}
}

func TestBootstrapWithoutTokenRef(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	account := storage.Account{
		ID:        "acct-1",
		Email:     "user@example.com",
		IsPrimary: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.UpsertAccount(ctx, &account); err != nil {
		t.Fatalf("UpsertAccount: %v", err)
	}

	svc, err := NewService(ctx, zap.NewNop(), &config.Config{}, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if svc.State().SignedIn {
		t.Fatal("expected SignedIn false without token ref")
	}
}

func TestBootstrapSelectsPrimaryWithToken(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	primary := storage.Account{
		ID:        "acct-primary",
		Email:     "primary@example.com",
		IsPrimary: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	secondary := storage.Account{
		ID:        "acct-secondary",
		Email:     "secondary@example.com",
		IsPrimary: false,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := store.UpsertAccount(ctx, &secondary); err != nil {
		t.Fatalf("UpsertAccount secondary: %v", err)
	}
	if err := store.UpsertAccount(ctx, &primary); err != nil {
		t.Fatalf("UpsertAccount primary: %v", err)
	}
	if err := store.UpsertTokenRef(ctx, &storage.TokenRef{AccountID: secondary.ID, KeyID: secondary.ID}); err != nil {
		t.Fatalf("UpsertTokenRef secondary: %v", err)
	}
	if err := store.UpsertTokenRef(ctx, &storage.TokenRef{AccountID: primary.ID, KeyID: primary.ID}); err != nil {
		t.Fatalf("UpsertTokenRef primary: %v", err)
	}

	svc, err := NewService(ctx, zap.NewNop(), &config.Config{}, store)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	state := svc.State()
	if !state.SignedIn || state.Account.ID != primary.ID {
		t.Fatalf("expected primary account, got %#v", state)
	}
}

func TestScopeStringDedupes(t *testing.T) {
	got := scopeString([]string{"b", "a", "b", "", "a"})
	if got != "a b" {
		t.Fatalf("unexpected scope string: %q", got)
	}
}

func TestDecodeJWTClaims(t *testing.T) {
	payload := idTokenClaims{
		Sub:   "sub-1",
		Email: "user@example.com",
		Name:  "User",
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	body := base64.RawURLEncoding.EncodeToString(rawPayload)
	token := header + "." + body + "."

	claims, err := decodeJWTClaims(token)
	if err != nil {
		t.Fatalf("decodeJWTClaims: %v", err)
	}
	if claims.Sub != payload.Sub || claims.Email != payload.Email || claims.Name != payload.Name {
		t.Fatalf("unexpected claims: %#v", claims)
	}
}

func TestPKCEPair(t *testing.T) {
	verifier, challenge, err := pkcePair()
	if err != nil {
		t.Fatalf("pkcePair: %v", err)
	}
	if verifier == "" || challenge == "" {
		t.Fatal("expected verifier and challenge")
	}
	if verifier == challenge {
		t.Fatal("expected verifier and challenge to differ")
	}
}
