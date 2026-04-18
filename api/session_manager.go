package api
// session_manager.go — Fintech-grade session lifecycle for SwiftFiat.
//
// ── What it provides ──────────────────────────────────────────────────────────
//  • Refresh Token Rotation (RTR): every /auth/refresh call consumes the old
//    refresh token and issues a new pair. Replayed tokens → instantly invalid.
//  • Multi-device session tracking: each login creates an independent session
//    capped at MaxSessions per user; oldest is evicted when cap is reached.
//  • Per-session blocking: BlockSession() revokes one device without touching
//    others (used by admin / fraud rules).
//  • Heartbeat: TouchSession() updates LastActiveAt on every authenticated
//    request, called as a non-blocking goroutine.
//
// ── Redis key layout ──────────────────────────────────────────────────────────
//  session:{sessionID}          STRING(JSON)  SessionMeta
//  user_sessions:{userID}       SET           active sessionIDs for user
//  refresh:{sha256(rawToken)}   STRING        sessionID  (one-time-use)
//  user:{userID}                STRING        latest access token (compat key)
 
import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
 
	basemodels "github.com/SwiftFiat/SwiftFiat-Backend/models"
	"github.com/SwiftFiat/SwiftFiat-Backend/services/redis"
	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	AccessTokenTTL  = 15 * time.Minute
	RefreshTokenTTL = 7 * 24 * time.Hour
	MaxSessions     = 5
)
 
// ── Types ─────────────────────────────────────────────────────────────────────
 
// SessionMeta is stored as JSON under session:{sessionID}.
type SessionMeta struct {
	SessionID    string    `json:"session_id"`
	UserID       int64     `json:"user_id"`
	UserTag      string    `json:"user_tag"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	IPAddress    string    `json:"ip_address"`
	UserAgent    string    `json:"user_agent"`
	DeviceName   string    `json:"device_name"`
	CreatedAt    time.Time `json:"created_at"`
	LastActiveAt time.Time `json:"last_active_at"`
	Blocked      bool      `json:"blocked"`
}

// SessionTokenPair is returned to the client on login / refresh.
type SessionTokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	SessionID    string    `json:"session_id"`
}
 
// ── SessionManager ────────────────────────────────────────────────────────────
 
type SessionManager struct {
	redis *redis.RedisService
}
 
func NewSessionManager(r *redis.RedisService) *SessionManager {
	return &SessionManager{redis: r}
}

// ── Core API ──────────────────────────────────────────────────────────────────
 
// CreateSession mints a new (accessToken, refreshToken) pair, persists session
// metadata, and registers the sessionID in the user's session set.
func (sm *SessionManager) CreateSession(
	ctx context.Context,
	tokenObj utils.TokenObject,
	ip, userAgent string,
) (*SessionTokenPair, error) {
 
	sessionID := uuid.NewString()
	tokenObj.SessionID = sessionID // embed session_id into the JWT
 
	accessToken, err := TokenController.CreateToken(tokenObj)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}
 
	rawRefresh, err := generateSecureToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	refreshHash := hashToken(rawRefresh)
 
	now := time.Now()
	meta := SessionMeta{
		SessionID:    sessionID,
		UserID:       tokenObj.UserID,
		UserTag:      tokenObj.UserTag,
		Email:        tokenObj.Email,
		Role:         tokenObj.Role,
		IPAddress:    ip,
		UserAgent:    userAgent,
		DeviceName:   parseDevice(userAgent),
		CreatedAt:    now,
		LastActiveAt: now,
	}
	metaJSON, _ := json.Marshal(meta)
 
	if err := sm.enforceSessionLimit(ctx, tokenObj.UserID); err != nil {
		return nil, err
	}
 
	pipe := sm.redis.Pipeline()
	pipe.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
	pipe.SAdd(ctx, smUserSessionsKey(tokenObj.UserID), sessionID)
	pipe.Expire(ctx, smUserSessionsKey(tokenObj.UserID), RefreshTokenTTL)
	pipe.Set(ctx, smRefreshKey(refreshHash), sessionID, RefreshTokenTTL)
	// Legacy compat: AuthenticatedMiddleware still validates user:{id} → token
	pipe.Set(ctx, fmt.Sprintf("user:%d", tokenObj.UserID), accessToken, RefreshTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}
 
	return &SessionTokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresAt:    now.Add(AccessTokenTTL),
		SessionID:    sessionID,
	}, nil
}
 
// RefreshSession implements Refresh Token Rotation (RTR).
func (sm *SessionManager) RefreshSession(
	ctx context.Context,
	rawRefreshToken, ip, userAgent string,
) (*SessionTokenPair, error) {
 
	refreshHash := hashToken(rawRefreshToken)
	sessionID, err := sm.redis.Get(ctx, smRefreshKey(refreshHash))
	if err != nil {
		return nil, ErrInvalidRefreshToken
	}
 
	meta, err := sm.loadSession(ctx, sessionID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	if meta.Blocked {
		return nil, ErrSessionBlocked
	}
 
	tokenObj := utils.TokenObject{
		UserID:    meta.UserID,
		UserTag:   meta.UserTag,
		Email:     meta.Email,
		Role:      meta.Role,
		SessionID: sessionID,
	}
	newAccess, err := TokenController.CreateToken(tokenObj)
	if err != nil {
		return nil, fmt.Errorf("create access token: %w", err)
	}
 
	newRaw, err := generateSecureToken(32)
	if err != nil {
		return nil, fmt.Errorf("generate refresh token: %w", err)
	}
	newHash := hashToken(newRaw)
 
	now := time.Now()
	meta.LastActiveAt = now
	meta.IPAddress = ip
	metaJSON, _ := json.Marshal(meta)
 
	pipe := sm.redis.Pipeline()
	pipe.Del(ctx, smRefreshKey(refreshHash)) // one-time-use: invalidate old
	pipe.Set(ctx, smRefreshKey(newHash), sessionID, RefreshTokenTTL)
	pipe.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
	pipe.Set(ctx, fmt.Sprintf("user:%d", meta.UserID), newAccess, RefreshTokenTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("redis pipeline: %w", err)
	}
 
	return &SessionTokenPair{
		AccessToken:  newAccess,
		RefreshToken: newRaw,
		ExpiresAt:    now.Add(AccessTokenTTL),
		SessionID:    sessionID,
	}, nil
}
 
// RevokeSession kills a single session (one-device logout).
func (sm *SessionManager) RevokeSession(ctx context.Context, userID int64, sessionID string) error {
	pipe := sm.redis.Pipeline()
	pipe.Del(ctx, smSessionKey(sessionID))
	pipe.SRem(ctx, smUserSessionsKey(userID), sessionID)
	_, err := pipe.Exec(ctx)
	return err
}
 
// RevokeAllUserSessions kills every session for a user (logout-all).
func (sm *SessionManager) RevokeAllUserSessions(ctx context.Context, userID int64) error {
	sessionIDs, err := sm.redis.SMembers(ctx, smUserSessionsKey(userID))
	if err != nil {
		return err
	}
	pipe := sm.redis.Pipeline()
	for _, sid := range sessionIDs {
		pipe.Del(ctx, smSessionKey(sid))
	}
	pipe.Del(ctx, smUserSessionsKey(userID))
	pipe.Del(ctx, fmt.Sprintf("user:%d", userID))
	_, err = pipe.Exec(ctx)
	return err
}
 
// BlockSession hard-blocks a session (admin / fraud action).
func (sm *SessionManager) BlockSession(ctx context.Context, sessionID string) error {
	meta, err := sm.loadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	meta.Blocked = true
	metaJSON, _ := json.Marshal(meta)
	return sm.redis.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
}
 
// GetUserSessions returns all active sessions for a user.
func (sm *SessionManager) GetUserSessions(ctx context.Context, userID int64) ([]SessionMeta, error) {
	sessionIDs, err := sm.redis.SMembers(ctx, smUserSessionsKey(userID))
	if err != nil {
		return nil, err
	}
	out := make([]SessionMeta, 0, len(sessionIDs))
	for _, sid := range sessionIDs {
		meta, err := sm.loadSession(ctx, sid)
		if err != nil {
			sm.redis.SRem(ctx, smUserSessionsKey(userID), sid) // stale pointer
			continue
		}
		out = append(out, *meta)
	}
	return out, nil
}
 
// TouchSession updates LastActiveAt (goroutine-safe; errors are silently dropped).
func (sm *SessionManager) TouchSession(ctx context.Context, sessionID string) {
	meta, err := sm.loadSession(ctx, sessionID)
	if err != nil {
		return
	}
	meta.LastActiveAt = time.Now()
	metaJSON, _ := json.Marshal(meta)
	sm.redis.Set(ctx, smSessionKey(sessionID), string(metaJSON), RefreshTokenTTL)
}
 
// ── HTTP handlers ─────────────────────────────────────────────────────────────
 
// HandleRefreshToken → POST /api/v1/auth/refresh
func (sm *SessionManager) HandleRefreshToken(server *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		var req struct {
			RefreshToken string `json:"refresh_token" binding:"required"`
		}
		if err := ctx.ShouldBindJSON(&req); err != nil {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("refresh_token is required"))
			return
		}
		pair, err := sm.RefreshSession(ctx.Request.Context(),
			req.RefreshToken, ctx.ClientIP(), ctx.Request.UserAgent())
		switch err {
		case nil:
			ctx.JSON(http.StatusOK, basemodels.NewSuccess("tokens refreshed", pair))
		case ErrInvalidRefreshToken:
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("invalid or expired refresh token"))
		case ErrSessionBlocked:
			ctx.JSON(http.StatusForbidden, basemodels.NewError("session has been revoked"))
		default:
			server.logger.Error(fmt.Sprintf("refresh session error: %v", err))
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("could not refresh token"))
		}
	}
}
 
// HandleListSessions → GET /api/v1/auth/sessions
func (sm *SessionManager) HandleListSessions(server *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		activeUser, err := utils.GetActiveUser(ctx)
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
			return
		}
		sessions, err := sm.GetUserSessions(ctx.Request.Context(), activeUser.UserID)
		if err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("could not list sessions"))
			return
		}
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("sessions retrieved", sessions))
	}
}
 
// HandleRevokeSession → DELETE /api/v1/auth/sessions/:session_id
func (sm *SessionManager) HandleRevokeSession(server *Server) gin.HandlerFunc {
	return func(ctx *gin.Context) {
		activeUser, err := utils.GetActiveUser(ctx)
		if err != nil {
			ctx.JSON(http.StatusUnauthorized, basemodels.NewError("unauthorized"))
			return
		}
		sessionID := ctx.Param("session_id")
		if sessionID == "" {
			ctx.JSON(http.StatusBadRequest, basemodels.NewError("session_id is required"))
			return
		}
		meta, err := sm.loadSession(ctx.Request.Context(), sessionID)
		if err != nil || meta.UserID != activeUser.UserID {
			ctx.JSON(http.StatusNotFound, basemodels.NewError("session not found"))
			return
		}
		if err := sm.RevokeSession(ctx.Request.Context(), activeUser.UserID, sessionID); err != nil {
			ctx.JSON(http.StatusInternalServerError, basemodels.NewError("could not revoke session"))
			return
		}
		ctx.JSON(http.StatusOK, basemodels.NewSuccess("session revoked", nil))
	}
}
 
// ── Internal helpers ──────────────────────────────────────────────────────────
 
func (sm *SessionManager) loadSession(ctx context.Context, sessionID string) (*SessionMeta, error) {
	raw, err := sm.redis.Get(ctx, smSessionKey(sessionID))
	if err != nil {
		return nil, ErrSessionNotFound
	}
	var meta SessionMeta
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return nil, fmt.Errorf("unmarshal session: %w", err)
	}
	return &meta, nil
}
 
func (sm *SessionManager) enforceSessionLimit(ctx context.Context, userID int64) error {
	sessionIDs, err := sm.redis.SMembers(ctx, smUserSessionsKey(userID))
	if err != nil || len(sessionIDs) < MaxSessions {
		return nil
	}
	var oldestID string
	oldestT := time.Now()
	for _, sid := range sessionIDs {
		meta, err := sm.loadSession(ctx, sid)
		if err != nil {
			sm.redis.SRem(ctx, smUserSessionsKey(userID), sid)
			return nil
		}
		if meta.CreatedAt.Before(oldestT) {
			oldestT = meta.CreatedAt
			oldestID = sid
		}
	}
	if oldestID != "" {
		pipe := sm.redis.Pipeline()
		pipe.Del(ctx, smSessionKey(oldestID))
		pipe.SRem(ctx, smUserSessionsKey(userID), oldestID)
		pipe.Exec(ctx)
	}
	return nil
}
 
// ── Key helpers ───────────────────────────────────────────────────────────────
 
func smSessionKey(id string) string    { return "session:" + id }
func smUserSessionsKey(uid int64) string { return fmt.Sprintf("user_sessions:%d", uid) }
func smRefreshKey(hash string) string  { return "refresh:" + hash }
 
// ── Crypto ────────────────────────────────────────────────────────────────────
 
func generateSecureToken(byteLen int) (string, error) {
	b := make([]byte, byteLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
 
func hashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
 
func parseDevice(ua string) string {
	for _, pair := range []struct{ sub, label string }{
		{"iPhone", "iOS (iPhone)"}, {"iPad", "iOS (iPad)"},
		{"Android", "Android"}, {"Windows", "Windows"},
		{"Macintosh", "macOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, pair.sub) {
			return pair.label
		}
	}
	return "Unknown Device"
}
 
// ── Sentinel errors ───────────────────────────────────────────────────────────
 
type sessionError string
 
func (e sessionError) Error() string { return string(e) }
 
const (
	ErrInvalidRefreshToken sessionError = "invalid or expired refresh token"
	ErrSessionNotFound     sessionError = "session not found"
	ErrSessionBlocked      sessionError = "session blocked"
)
 

