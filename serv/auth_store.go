package serv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3/oidc"
	"github.com/redis/go-redis/v9"
)

// Built-in login state (device sessions, OIDC state, MCP OAuth clients,
// authorization codes, refresh tokens) lives behind authStateStore. With the
// artifact store enabled it is a table in the control-plane store so every
// replica sees the same state and it survives restarts; without one it falls
// back to the per-process maps on authLoginService.

const (
	authStoreTable = "auth_sessions"

	authKindDevice       = "device"
	authKindOAuthClient  = "oauth_client"
	authKindOAuthRequest = "oauth_request"
	authKindOAuthCode    = "oauth_code"
	authKindRefreshToken = "refresh_token"

	// CIMD documents are re-fetched once the cached copy expires; DCR clients
	// have no other source of truth and never expire.
	mcpOAuthCIMDCacheTTL = 24 * time.Hour

	authStoreCASAttempts  = 3
	authStoreCacheMaxTTL  = 5 * time.Minute
	authStoreCacheTimeout = 100 * time.Millisecond
	authStoreCachePrefix  = "gj:auth:"
)

var (
	errAuthSessionUnknown = errors.New("auth session not found")
	errAuthSessionExpired = errors.New("auth session expired")
	errAuthStoreConflict  = errors.New("auth session changed concurrently")
)

type devicePollOutcome int

const (
	devicePollUnknown devicePollOutcome = iota
	devicePollExpired
	devicePollSlowDown
	devicePollPending
	devicePollCompleted // this poll redeemed the session
	devicePollRedeemed  // an earlier poll already redeemed it
	devicePollDenied
)

type devicePollResult struct {
	Outcome devicePollOutcome
	Session deviceSession
}

// authStateStore is the persistence boundary for built-in login state.
// Device sessions are addressed by an opaque ref returned from
// lookupUserCode / deviceByState; only pollDevice takes the raw device code.
// take* methods are single-use: of any number of concurrent callers, at most
// one gets the record.
type authStateStore interface {
	createDevice(ctx context.Context, s *deviceSession) error
	lookupUserCode(ctx context.Context, userCode string, now time.Time) (string, bool, error)
	bindDeviceState(ctx context.Context, ref, state string, now time.Time) error
	deviceByState(ctx context.Context, state string, now time.Time) (string, error)
	completeDevice(ctx context.Context, ref, state string, ident *oidc.Identity) error
	denyDevice(ctx context.Context, ref, state, reason string) error
	pollDevice(ctx context.Context, deviceCode string, now time.Time) (devicePollResult, error)

	putOAuthClient(ctx context.Context, c *mcpOAuthClient, expiresAt time.Time) error
	getOAuthClient(ctx context.Context, clientID string, now time.Time) (*mcpOAuthClient, error)
	putOAuthRequest(ctx context.Context, state string, req *mcpOAuthAuthRequest) error
	takeOAuthRequest(ctx context.Context, state string) (*mcpOAuthAuthRequest, error)
	putOAuthCode(ctx context.Context, code string, c *mcpOAuthCode) error
	takeOAuthCode(ctx context.Context, code string) (*mcpOAuthCode, error)
	putRefreshToken(ctx context.Context, token string, rt *mcpOAuthRefreshToken) error
	getRefreshToken(ctx context.Context, token string, now time.Time) (*mcpOAuthRefreshToken, error)

	sweep(ctx context.Context, now time.Time) error
}

// state returns the configured store, or the in-process maps.
func (a *authLoginService) state() authStateStore {
	if a.store != nil {
		return a.store
	}
	return memoryAuthStore{a: a}
}

func (s *graphjinService) authStoreEnabled() bool {
	return s != nil && s.conf != nil && s.conf.AuthLogin.Enabled && s.conf.Core.Artifacts.Enabled
}

// initAuthLogin builds the built-in login service on top of the shared
// artifact store when one is configured.
func (s *graphjinService) initAuthLogin(ctx context.Context) (*authLoginService, error) {
	store, err := s.openAuthStateStore(ctx)
	if err != nil {
		return nil, err
	}
	return newAuthLoginServiceWithStore(ctx, s.conf, store)
}

func (s *graphjinService) openAuthStateStore(ctx context.Context) (authStateStore, error) {
	if !s.authStoreEnabled() {
		if s != nil && s.log != nil && s.conf != nil && s.conf.AuthLogin.Enabled {
			s.log.Warn("auth_login: artifact store disabled — login state is per-process and lost on restart")
		}
		return nil, nil
	}
	db, dbType, _, ok := s.artifactDB()
	if !ok {
		if s.log != nil {
			s.log.Warn("auth_login: artifact store database is not configured — login state is per-process")
		}
		return nil, nil
	}
	schema := s.conf.Core.EffectiveArtifactsConfig().Schema
	if s.conf.Core.Artifacts.AutoInitEnabled() {
		for _, stmt := range authStoreDDL(dbType, schema) {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return nil, fmt.Errorf("auth_login: create auth store: %w", err)
			}
		}
		if err := migrateAuthStoreSchema(ctx, db, dbType, schema); err != nil {
			return nil, fmt.Errorf("auth_login: migrate auth store: %w", err)
		}
	}
	st := newSQLAuthStore(db, dbType, schema)
	if s.conf.Redis.URL != "" {
		client, err := newAuthStoreRedis(ctx, s.conf.Redis.URL)
		if err != nil {
			if s.log != nil {
				s.log.Warnf("auth_login: Redis unavailable, reading login state from the store only: %s", err)
			}
		} else {
			st.cache = client
		}
	}
	return st, nil
}

func newAuthStoreRedis(ctx context.Context, url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		client.Close() //nolint:errcheck
		return nil, err
	}
	return client, nil
}

func authStoreDDL(dbType, schema string) []string {
	table := artifactTableName(dbType, schema, authStoreTable)
	switch dbType {
	case "postgres", "":
		return []string{
			fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quotePGIdent(schema)),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
kind TEXT NOT NULL,
id TEXT NOT NULL,
lookup_key TEXT NOT NULL DEFAULT '',
state_key TEXT NOT NULL DEFAULT '',
data TEXT NOT NULL,
version BIGINT NOT NULL DEFAULT 0,
expires_at BIGINT NOT NULL DEFAULT 0,
created_at BIGINT NOT NULL DEFAULT 0,
updated_at BIGINT NOT NULL DEFAULT 0,
PRIMARY KEY (kind, id)
)`, table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (kind, lookup_key)`, quotePGIdent("idx_graphjin_auth_sessions_lookup"), table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (kind, state_key)`, quotePGIdent("idx_graphjin_auth_sessions_state"), table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (expires_at)`, quotePGIdent("idx_graphjin_auth_sessions_expires"), table),
		}
	case "mysql", "mariadb":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
kind VARCHAR(32) NOT NULL,
id VARCHAR(191) NOT NULL,
lookup_key VARCHAR(191) NOT NULL DEFAULT '',
state_key VARCHAR(191) NOT NULL DEFAULT '',
data LONGTEXT NOT NULL,
version BIGINT NOT NULL DEFAULT 0,
expires_at BIGINT NOT NULL DEFAULT 0,
created_at BIGINT NOT NULL DEFAULT 0,
updated_at BIGINT NOT NULL DEFAULT 0,
PRIMARY KEY (kind, id),
KEY idx_graphjin_auth_sessions_lookup (kind, lookup_key),
KEY idx_graphjin_auth_sessions_state (kind, state_key),
KEY idx_graphjin_auth_sessions_expires (expires_at)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, table),
		}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
kind TEXT NOT NULL,
id TEXT NOT NULL,
lookup_key TEXT NOT NULL DEFAULT '',
state_key TEXT NOT NULL DEFAULT '',
data TEXT NOT NULL,
version INTEGER NOT NULL DEFAULT 0,
expires_at INTEGER NOT NULL DEFAULT 0,
created_at INTEGER NOT NULL DEFAULT 0,
updated_at INTEGER NOT NULL DEFAULT 0,
PRIMARY KEY (kind, id)
)`, table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (kind, lookup_key)`, quoteSQLIdent("idx_graphjin_auth_sessions_lookup"), table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (kind, state_key)`, quoteSQLIdent("idx_graphjin_auth_sessions_state"), table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (expires_at)`, quoteSQLIdent("idx_graphjin_auth_sessions_expires"), table),
		}
	}
}

// migrateAuthStoreSchema adds columns introduced after the table's first
// release. New columns go here as well as in authStoreDDL.
func migrateAuthStoreSchema(ctx context.Context, db *sql.DB, dbType, schema string) error {
	table := artifactTableName(dbType, schema, authStoreTable)
	columns := []struct{ name, definition string }{
		{"lookup_key", "TEXT NOT NULL DEFAULT ''"},
		{"state_key", "TEXT NOT NULL DEFAULT ''"},
		{"version", "INTEGER NOT NULL DEFAULT 0"},
		{"updated_at", "INTEGER NOT NULL DEFAULT 0"},
	}
	switch dbType {
	case "postgres", "":
		columns[2].definition = "BIGINT NOT NULL DEFAULT 0"
		columns[3].definition = "BIGINT NOT NULL DEFAULT 0"
	case "mysql", "mariadb":
		columns[0].definition = "VARCHAR(191) NOT NULL DEFAULT ''"
		columns[1].definition = "VARCHAR(191) NOT NULL DEFAULT ''"
		columns[2].definition = "BIGINT NOT NULL DEFAULT 0"
		columns[3].definition = "BIGINT NOT NULL DEFAULT 0"
	}
	for _, column := range columns {
		if err := ensureSQLColumn(ctx, db, dbType, table, column.name, column.definition); err != nil {
			return err
		}
	}
	return nil
}

// authStoreKey hashes a bearer secret (device code, state, authorization
// code, refresh token) before it is used as a row key, so the store never
// holds a value that can be presented back to GraphJin.
func authStoreKey(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// storedIdentity drops the raw id_token claims; only the fields used to mint
// local tokens are persisted.
func storedIdentity(ident *oidc.Identity) *oidc.Identity {
	if ident == nil {
		return nil
	}
	out := *ident
	out.Raw = nil
	return &out
}

func unixMilli(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// ---- SQL store ----

type sqlAuthStore struct {
	db     *sql.DB
	dbType string
	table  string
	// cache optionally fronts reads of records that never change after they
	// are written (OAuth clients, refresh tokens). The store stays the source
	// of truth; single-use records are never cached.
	cache *redis.Client
}

type authStoreRow struct {
	id        string
	stateKey  string
	data      []byte
	version   int64
	expiresAt int64
}

func (r *authStoreRow) expired(now time.Time) bool {
	return r.expiresAt > 0 && now.UnixMilli() >= r.expiresAt
}

func newSQLAuthStore(db *sql.DB, dbType, schema string) *sqlAuthStore {
	return &sqlAuthStore{db: db, dbType: dbType, table: artifactTableName(dbType, schema, authStoreTable)}
}

// q expands {t} to the table name and rebinds ? placeholders for postgres.
func (st *sqlAuthStore) q(query string) string {
	query = strings.ReplaceAll(query, "{t}", st.table)
	if st.dbType != "postgres" && st.dbType != "" {
		return query
	}
	var b strings.Builder
	n := 0
	for _, c := range query {
		if c == '?' {
			n++
			b.WriteString("$" + strconv.Itoa(n))
			continue
		}
		b.WriteRune(c)
	}
	return b.String()
}

func (st *sqlAuthStore) get(ctx context.Context, kind, id string) (*authStoreRow, error) {
	return st.scanRow(st.db.QueryRowContext(ctx,
		st.q(`SELECT id, state_key, data, version, expires_at FROM {t} WHERE kind = ? AND id = ?`), kind, id))
}

// getBy looks a row up by one of the secondary key columns.
func (st *sqlAuthStore) getBy(ctx context.Context, kind, column, value string) (*authStoreRow, error) {
	if column != "lookup_key" && column != "state_key" {
		return nil, fmt.Errorf("auth store: unsupported lookup column %q", column)
	}
	return st.scanRow(st.db.QueryRowContext(ctx,
		st.q(`SELECT id, state_key, data, version, expires_at FROM {t} WHERE kind = ? AND `+column+` = ? ORDER BY created_at DESC LIMIT 1`), kind, value))
}

func (st *sqlAuthStore) scanRow(row *sql.Row) (*authStoreRow, error) {
	var r authStoreRow
	var data string
	if err := row.Scan(&r.id, &r.stateKey, &data, &r.version, &r.expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	r.data = []byte(data)
	return &r, nil
}

func (st *sqlAuthStore) insert(ctx context.Context, kind, id, lookupKey string, v any, expiresAt time.Time) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = st.db.ExecContext(ctx,
		st.q(`INSERT INTO {t} (kind, id, lookup_key, state_key, data, version, expires_at, created_at, updated_at) VALUES (?, ?, ?, '', ?, 0, ?, ?, ?)`),
		kind, id, lookupKey, string(data), unixMilli(expiresAt), now, now)
	return err
}

func (st *sqlAuthStore) upsert(ctx context.Context, kind, id string, v any, expiresAt time.Time) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	query := `INSERT INTO {t} (kind, id, lookup_key, state_key, data, version, expires_at, created_at, updated_at) VALUES (?, ?, '', '', ?, 0, ?, ?, ?) `
	switch st.dbType {
	case "mysql", "mariadb":
		query += `ON DUPLICATE KEY UPDATE data = VALUES(data), expires_at = VALUES(expires_at), updated_at = VALUES(updated_at), version = version + 1`
	default:
		query += `ON CONFLICT (kind, id) DO UPDATE SET data = excluded.data, expires_at = excluded.expires_at, updated_at = excluded.updated_at, version = {t}.version + 1`
	}
	_, err = st.db.ExecContext(ctx, st.q(query), kind, id, string(data), unixMilli(expiresAt), now, now)
	return err
}

// cas replaces a row's data (and state_key) only if nobody else has written
// it since it was read at version.
func (st *sqlAuthStore) cas(ctx context.Context, kind string, row *authStoreRow, v any, stateKey string) (bool, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return false, err
	}
	res, err := st.db.ExecContext(ctx,
		st.q(`UPDATE {t} SET data = ?, state_key = ?, version = version + 1, updated_at = ? WHERE kind = ? AND id = ? AND version = ?`),
		string(data), stateKey, time.Now().UnixMilli(), kind, row.id, row.version)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// del reports whether this call removed the row, which is what makes take*
// single-use across replicas.
func (st *sqlAuthStore) del(ctx context.Context, kind, id string) (bool, error) {
	res, err := st.db.ExecContext(ctx, st.q(`DELETE FROM {t} WHERE kind = ? AND id = ?`), kind, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// take reads and deletes a row; it returns nil when the row is missing or
// another caller deleted it first.
func (st *sqlAuthStore) take(ctx context.Context, kind, id string, v any) (bool, error) {
	row, err := st.get(ctx, kind, id)
	if err != nil || row == nil {
		return false, err
	}
	won, err := st.del(ctx, kind, id)
	if err != nil || !won {
		return false, err
	}
	return true, json.Unmarshal(row.data, v)
}

// updateDevice applies fn to the device session at ref under CAS, retrying
// a few times if another replica wrote it in between.
func (st *sqlAuthStore) updateDevice(ctx context.Context, ref string, fn func(*authStoreRow, *deviceSession) (string, error)) error {
	for i := 0; i < authStoreCASAttempts; i++ {
		row, err := st.get(ctx, authKindDevice, ref)
		if err != nil {
			return err
		}
		if row == nil {
			return errAuthSessionUnknown
		}
		var s deviceSession
		if err := json.Unmarshal(row.data, &s); err != nil {
			return err
		}
		stateKey, err := fn(row, &s)
		if err != nil {
			return err
		}
		ok, err := st.cas(ctx, authKindDevice, row, &s, stateKey)
		if err != nil || ok {
			return err
		}
	}
	return errAuthStoreConflict
}

func (st *sqlAuthStore) createDevice(ctx context.Context, s *deviceSession) error {
	stored := *s
	stored.Identity = storedIdentity(s.Identity)
	return st.insert(ctx, authKindDevice, authStoreKey(s.DeviceCode), authStoreKey(s.UserCode), &stored, s.ExpiresAt)
}

func (st *sqlAuthStore) lookupUserCode(ctx context.Context, userCode string, now time.Time) (string, bool, error) {
	row, err := st.getBy(ctx, authKindDevice, "lookup_key", authStoreKey(userCode))
	if err != nil || row == nil || row.expired(now) {
		return "", false, err
	}
	return row.id, true, nil
}

func (st *sqlAuthStore) bindDeviceState(ctx context.Context, ref, state string, now time.Time) error {
	err := st.updateDevice(ctx, ref, func(row *authStoreRow, s *deviceSession) (string, error) {
		if row.expired(now) {
			return "", errAuthSessionExpired
		}
		s.Status = deviceStatusVerified
		return authStoreKey(state), nil
	})
	if errors.Is(err, errAuthSessionUnknown) {
		return errAuthSessionExpired
	}
	return err
}

func (st *sqlAuthStore) deviceByState(ctx context.Context, state string, now time.Time) (string, error) {
	row, err := st.getBy(ctx, authKindDevice, "state_key", authStoreKey(state))
	if err != nil {
		return "", err
	}
	if row == nil {
		return "", errAuthSessionUnknown
	}
	if row.expired(now) {
		return "", errAuthSessionExpired
	}
	return row.id, nil
}

func (st *sqlAuthStore) completeDevice(ctx context.Context, ref, state string, ident *oidc.Identity) error {
	return st.updateDevice(ctx, ref, func(row *authStoreRow, s *deviceSession) (string, error) {
		// A newer redirect replaced this state; let that sign-in win.
		if row.stateKey != authStoreKey(state) {
			return "", errAuthSessionUnknown
		}
		s.Status = deviceStatusCompleted
		s.Identity = storedIdentity(ident)
		return "", nil
	})
}

func (st *sqlAuthStore) denyDevice(ctx context.Context, ref, state, reason string) error {
	err := st.updateDevice(ctx, ref, func(_ *authStoreRow, s *deviceSession) (string, error) {
		s.Status = deviceStatusDenied
		s.DeniedError = reason
		return "", nil
	})
	if errors.Is(err, errAuthSessionUnknown) {
		return nil
	}
	return err
}

func (st *sqlAuthStore) pollDevice(ctx context.Context, deviceCode string, now time.Time) (devicePollResult, error) {
	id := authStoreKey(deviceCode)
	row, err := st.get(ctx, authKindDevice, id)
	if err != nil {
		return devicePollResult{}, err
	}
	if row == nil {
		return devicePollResult{Outcome: devicePollUnknown}, nil
	}
	if row.expired(now) {
		_, err := st.del(ctx, authKindDevice, id)
		return devicePollResult{Outcome: devicePollExpired}, err
	}
	var s deviceSession
	if err := json.Unmarshal(row.data, &s); err != nil {
		return devicePollResult{}, err
	}
	if !s.LastPollAt.IsZero() && now.Sub(s.LastPollAt) < deviceCodeInterval {
		return devicePollResult{Outcome: devicePollSlowDown, Session: s}, nil
	}
	prior := s.Status
	s.LastPollAt = now
	if prior == deviceStatusCompleted {
		s.Status = deviceStatusRedeemed
	}
	ok, err := st.cas(ctx, authKindDevice, row, &s, row.stateKey)
	if err != nil {
		return devicePollResult{}, err
	}
	if !ok {
		// Another replica handled a poll for this session in between. Only
		// that one may redeem it; this client just backs off.
		return devicePollResult{Outcome: devicePollSlowDown}, nil
	}
	return devicePollResult{Outcome: devicePollOutcomeFor(prior), Session: s}, nil
}

func devicePollOutcomeFor(status deviceSessionStatus) devicePollOutcome {
	switch status {
	case deviceStatusPending, deviceStatusVerified:
		return devicePollPending
	case deviceStatusCompleted:
		return devicePollCompleted
	case deviceStatusRedeemed:
		return devicePollRedeemed
	case deviceStatusDenied:
		return devicePollDenied
	default:
		return devicePollUnknown
	}
}

func (st *sqlAuthStore) putOAuthClient(ctx context.Context, c *mcpOAuthClient, expiresAt time.Time) error {
	id := authStoreKey(c.ClientID)
	if err := st.upsert(ctx, authKindOAuthClient, id, c, expiresAt); err != nil {
		return err
	}
	st.cacheDel(ctx, authKindOAuthClient, id)
	return nil
}

func (st *sqlAuthStore) getOAuthClient(ctx context.Context, clientID string, now time.Time) (*mcpOAuthClient, error) {
	var c mcpOAuthClient
	ok, err := st.cachedGet(ctx, authKindOAuthClient, authStoreKey(clientID), now, &c)
	if err != nil || !ok {
		return nil, err
	}
	return &c, nil
}

func (st *sqlAuthStore) putOAuthRequest(ctx context.Context, state string, req *mcpOAuthAuthRequest) error {
	return st.insert(ctx, authKindOAuthRequest, authStoreKey(state), "", req, req.ExpiresAt)
}

func (st *sqlAuthStore) takeOAuthRequest(ctx context.Context, state string) (*mcpOAuthAuthRequest, error) {
	var req mcpOAuthAuthRequest
	ok, err := st.take(ctx, authKindOAuthRequest, authStoreKey(state), &req)
	if err != nil || !ok {
		return nil, err
	}
	return &req, nil
}

func (st *sqlAuthStore) putOAuthCode(ctx context.Context, code string, c *mcpOAuthCode) error {
	stored := *c
	stored.Identity = storedIdentity(c.Identity)
	return st.insert(ctx, authKindOAuthCode, authStoreKey(code), "", &stored, c.ExpiresAt)
}

func (st *sqlAuthStore) takeOAuthCode(ctx context.Context, code string) (*mcpOAuthCode, error) {
	var c mcpOAuthCode
	ok, err := st.take(ctx, authKindOAuthCode, authStoreKey(code), &c)
	if err != nil || !ok {
		return nil, err
	}
	return &c, nil
}

func (st *sqlAuthStore) putRefreshToken(ctx context.Context, token string, rt *mcpOAuthRefreshToken) error {
	stored := *rt
	stored.Identity = storedIdentity(rt.Identity)
	return st.insert(ctx, authKindRefreshToken, authStoreKey(token), "", &stored, rt.ExpiresAt)
}

func (st *sqlAuthStore) getRefreshToken(ctx context.Context, token string, now time.Time) (*mcpOAuthRefreshToken, error) {
	var rt mcpOAuthRefreshToken
	ok, err := st.cachedGet(ctx, authKindRefreshToken, authStoreKey(token), now, &rt)
	if err != nil || !ok {
		return nil, err
	}
	return &rt, nil
}

func (st *sqlAuthStore) sweep(ctx context.Context, now time.Time) error {
	_, err := st.db.ExecContext(ctx, st.q(`DELETE FROM {t} WHERE expires_at > 0 AND expires_at <= ?`), now.UnixMilli())
	return err
}

// cachedGet reads an immutable record through the optional Redis cache.
// Cache errors fall through to the store.
func (st *sqlAuthStore) cachedGet(ctx context.Context, kind, id string, now time.Time, v any) (bool, error) {
	if st.cache != nil {
		cctx, cancel := context.WithTimeout(ctx, authStoreCacheTimeout)
		data, err := st.cache.Get(cctx, authStoreCachePrefix+kind+":"+id).Bytes()
		cancel()
		if err == nil && json.Unmarshal(data, v) == nil {
			return true, nil
		}
	}
	row, err := st.get(ctx, kind, id)
	if err != nil || row == nil {
		return false, err
	}
	if row.expired(now) {
		_, err := st.del(ctx, kind, id)
		return false, err
	}
	if err := json.Unmarshal(row.data, v); err != nil {
		return false, err
	}
	if st.cache != nil {
		// Never cache past the row's own expiry, so a swept row cannot be
		// served from Redis.
		ttl := authStoreCacheMaxTTL
		if row.expiresAt > 0 {
			if left := time.Duration(row.expiresAt-now.UnixMilli()) * time.Millisecond; left < ttl {
				ttl = left
			}
		}
		if ttl > 0 {
			cctx, cancel := context.WithTimeout(ctx, authStoreCacheTimeout)
			st.cache.Set(cctx, authStoreCachePrefix+kind+":"+id, row.data, ttl) //nolint:errcheck
			cancel()
		}
	}
	return true, nil
}

func (st *sqlAuthStore) cacheDel(ctx context.Context, kind, id string) {
	if st.cache == nil {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, authStoreCacheTimeout)
	st.cache.Del(cctx, authStoreCachePrefix+kind+":"+id) //nolint:errcheck
	cancel()
}

// ---- in-memory fallback ----

// memoryAuthStore keeps state in authLoginService's maps; device refs are the
// raw device codes.
type memoryAuthStore struct{ a *authLoginService }

func (m memoryAuthStore) lock() *authLoginService {
	a := m.a
	a.mu.Lock()
	if a.byDevice == nil {
		a.byDevice = map[string]*deviceSession{}
	}
	if a.byUser == nil {
		a.byUser = map[string]string{}
	}
	if a.byState == nil {
		a.byState = map[string]string{}
	}
	if a.oauthClients == nil {
		a.oauthClients = map[string]*mcpOAuthClient{}
	}
	if a.oauthStates == nil {
		a.oauthStates = map[string]*mcpOAuthAuthRequest{}
	}
	if a.oauthCodes == nil {
		a.oauthCodes = map[string]*mcpOAuthCode{}
	}
	if a.oauthRefreshTokens == nil {
		a.oauthRefreshTokens = map[string]*mcpOAuthRefreshToken{}
	}
	return a
}

func (m memoryAuthStore) createDevice(_ context.Context, s *deviceSession) error {
	a := m.lock()
	defer a.mu.Unlock()
	a.byDevice[s.DeviceCode] = s
	a.byUser[s.UserCode] = s.DeviceCode
	return nil
}

func (m memoryAuthStore) lookupUserCode(_ context.Context, userCode string, now time.Time) (string, bool, error) {
	a := m.lock()
	defer a.mu.Unlock()
	dc, ok := a.byUser[userCode]
	if !ok {
		return "", false, nil
	}
	s, ok := a.byDevice[dc]
	if !ok || now.After(s.ExpiresAt) {
		return "", false, nil
	}
	return dc, true, nil
}

func (m memoryAuthStore) bindDeviceState(_ context.Context, dc, state string, now time.Time) error {
	a := m.lock()
	defer a.mu.Unlock()
	s, ok := a.byDevice[dc]
	if !ok || now.After(s.ExpiresAt) {
		return errAuthSessionExpired
	}
	// Invalidate any previous state.
	if s.OIDCState != "" {
		delete(a.byState, s.OIDCState)
	}
	s.OIDCState = state
	s.Status = deviceStatusVerified
	a.byState[state] = dc
	return nil
}

func (m memoryAuthStore) deviceByState(_ context.Context, state string, now time.Time) (string, error) {
	a := m.lock()
	defer a.mu.Unlock()
	dc, ok := a.byState[state]
	if !ok {
		return "", errAuthSessionUnknown
	}
	s := a.byDevice[dc]
	if s == nil || now.After(s.ExpiresAt) {
		delete(a.byState, state)
		return "", errAuthSessionExpired
	}
	return dc, nil
}

func (m memoryAuthStore) completeDevice(_ context.Context, dc, state string, ident *oidc.Identity) error {
	a := m.lock()
	defer a.mu.Unlock()
	if s := a.byDevice[dc]; s != nil {
		s.Status = deviceStatusCompleted
		s.Identity = ident
		s.OIDCState = ""
	}
	delete(a.byState, state)
	return nil
}

func (m memoryAuthStore) denyDevice(_ context.Context, dc, state, reason string) error {
	a := m.lock()
	defer a.mu.Unlock()
	if s, ok := a.byDevice[dc]; ok {
		s.Status = deviceStatusDenied
		s.DeniedError = reason
	}
	delete(a.byState, state)
	return nil
}

func (m memoryAuthStore) pollDevice(_ context.Context, deviceCode string, now time.Time) (devicePollResult, error) {
	a := m.lock()
	defer a.mu.Unlock()
	s, ok := a.byDevice[deviceCode]
	if !ok {
		return devicePollResult{Outcome: devicePollUnknown}, nil
	}
	if now.After(s.ExpiresAt) {
		delete(a.byDevice, deviceCode)
		delete(a.byUser, s.UserCode)
		return devicePollResult{Outcome: devicePollExpired}, nil
	}
	// Crude slow_down: enforce minimum interval between polls on the same
	// session (RFC 8628 §3.5 says server MUST bump interval by 5s but in
	// practice consumers just handle slow_down by waiting `interval` more).
	if !s.LastPollAt.IsZero() && now.Sub(s.LastPollAt) < deviceCodeInterval {
		return devicePollResult{Outcome: devicePollSlowDown, Session: *s}, nil
	}
	s.LastPollAt = now
	prior := s.Status
	if prior == deviceStatusCompleted {
		s.Status = deviceStatusRedeemed
	}
	return devicePollResult{Outcome: devicePollOutcomeFor(prior), Session: *s}, nil
}

func (m memoryAuthStore) putOAuthClient(_ context.Context, c *mcpOAuthClient, _ time.Time) error {
	a := m.lock()
	defer a.mu.Unlock()
	a.oauthClients[c.ClientID] = c
	return nil
}

func (m memoryAuthStore) getOAuthClient(_ context.Context, clientID string, _ time.Time) (*mcpOAuthClient, error) {
	a := m.lock()
	defer a.mu.Unlock()
	return a.oauthClients[clientID], nil
}

func (m memoryAuthStore) putOAuthRequest(_ context.Context, state string, req *mcpOAuthAuthRequest) error {
	a := m.lock()
	defer a.mu.Unlock()
	a.oauthStates[state] = req
	return nil
}

func (m memoryAuthStore) takeOAuthRequest(_ context.Context, state string) (*mcpOAuthAuthRequest, error) {
	a := m.lock()
	defer a.mu.Unlock()
	req := a.oauthStates[state]
	delete(a.oauthStates, state)
	return req, nil
}

func (m memoryAuthStore) putOAuthCode(_ context.Context, code string, c *mcpOAuthCode) error {
	a := m.lock()
	defer a.mu.Unlock()
	a.oauthCodes[code] = c
	return nil
}

func (m memoryAuthStore) takeOAuthCode(_ context.Context, code string) (*mcpOAuthCode, error) {
	a := m.lock()
	defer a.mu.Unlock()
	c := a.oauthCodes[code]
	delete(a.oauthCodes, code)
	return c, nil
}

func (m memoryAuthStore) putRefreshToken(_ context.Context, token string, rt *mcpOAuthRefreshToken) error {
	a := m.lock()
	defer a.mu.Unlock()
	a.oauthRefreshTokens[token] = rt
	return nil
}

func (m memoryAuthStore) getRefreshToken(_ context.Context, token string, now time.Time) (*mcpOAuthRefreshToken, error) {
	a := m.lock()
	defer a.mu.Unlock()
	rt := a.oauthRefreshTokens[token]
	if rt != nil && now.After(rt.ExpiresAt) {
		delete(a.oauthRefreshTokens, token)
		return nil, nil
	}
	return rt, nil
}

func (m memoryAuthStore) sweep(_ context.Context, now time.Time) error {
	a := m.lock()
	defer a.mu.Unlock()
	for dc, s := range a.byDevice {
		if now.After(s.ExpiresAt) {
			delete(a.byDevice, dc)
			delete(a.byUser, s.UserCode)
			if s.OIDCState != "" {
				delete(a.byState, s.OIDCState)
			}
		}
	}
	for state, s := range a.oauthStates {
		if now.After(s.ExpiresAt) {
			delete(a.oauthStates, state)
		}
	}
	for code, s := range a.oauthCodes {
		if now.After(s.ExpiresAt) {
			delete(a.oauthCodes, code)
		}
	}
	for tok, s := range a.oauthRefreshTokens {
		if now.After(s.ExpiresAt) {
			delete(a.oauthRefreshTokens, tok)
		}
	}
	return nil
}
