package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/arts-smart/strategy/internal/domain"
	"github.com/arts-smart/strategy/internal/repository"
)

// AuthStore is the persistence boundary required by authentication use cases.
type AuthStore interface {
	HasOwner(context.Context) (bool, error)
	SetupOwner(context.Context, string, string, string, string) (domain.User, error)
	RegisterTeam(context.Context, string, string, string, string) (domain.User, error)
	GetUserByEmail(context.Context, string) (domain.User, error)
	GetUser(context.Context, int64) (domain.User, error)
	GetSession(context.Context, string, int64) (time.Time, bool, error)
	CreateSession(context.Context, string, int64, time.Time) error
	RevokeSession(context.Context, string, int64) error
}

// AuthService provides account lifecycle, signed sessions and authorization.
type AuthService struct {
	Store  AuthStore
	Signer TokenSigner
}

// LoginResult preserves the existing access_token/user response contract.
type LoginResult struct {
	AccessToken string         `json:"access_token"`
	TokenType   string         `json:"token_type"`
	User        map[string]any `json:"user"`
}

// Setup creates the first account and its default team.
func (s AuthService) Setup(
	ctx context.Context,
	team, name, email, password string,
) (LoginResult, error) {
	if err := validateCredentials(name, email, password); err != nil {
		return LoginResult{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return LoginResult{}, err
	}
	user, err := s.Store.SetupOwner(ctx, team, email, name, hash)
	if err != nil {
		return LoginResult{}, err
	}
	return s.issue(ctx, user)
}

// Register creates a separate team and owner account.
func (s AuthService) Register(
	ctx context.Context,
	team, name, email, password string,
) (LoginResult, error) {
	if err := validateCredentials(name, email, password); err != nil {
		return LoginResult{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return LoginResult{}, err
	}
	user, err := s.Store.RegisterTeam(ctx, team, email, name, hash)
	if err != nil {
		return LoginResult{}, err
	}
	return s.issue(ctx, user)
}

// Login validates the legacy password hash and persists a revocable session.
func (s AuthService) Login(
	ctx context.Context,
	email, password string,
) (LoginResult, error) {
	user, err := s.Store.GetUserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, sql.ErrNoRows) || err == nil && !VerifyPassword(password, user.PasswordHash) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("%w: 读取账号失败: %v", ErrAuthBackend, err)
	}
	return s.issue(ctx, user)
}

// Authenticate verifies token signature, expiry, stored session and team scope.
func (s AuthService) Authenticate(
	ctx context.Context,
	token string,
	requestedTeamID int64,
) (domain.Claims, error) {
	payload, err := s.Signer.Verify(token, time.Now())
	if err != nil {
		return domain.Claims{}, ErrUnauthenticated
	}
	if requestedTeamID != 0 && requestedTeamID != payload.TeamID {
		return domain.Claims{}, ErrForbidden
	}
	expires, revoked, err := s.Store.GetSession(
		ctx,
		payload.SessionID,
		payload.UserID,
	)
	if errors.Is(err, sql.ErrNoRows) || revoked || err == nil && !expires.After(time.Now()) {
		return domain.Claims{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Claims{}, fmt.Errorf("%w: 读取登录会话失败: %v", ErrAuthBackend, err)
	}
	user, err := s.Store.GetUser(ctx, payload.UserID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && user.TeamID != payload.TeamID {
		return domain.Claims{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Claims{}, fmt.Errorf("%w: 读取会话账号失败: %v", ErrAuthBackend, err)
	}
	return domain.Claims{
		UserID:    user.ID,
		TeamID:    user.TeamID,
		Role:      user.Role,
		Name:      user.Name,
		Email:     user.Email,
		SessionID: payload.SessionID,
	}, nil
}

// Logout revokes the authenticated session.
func (s AuthService) Logout(ctx context.Context, claims domain.Claims) error {
	return s.Store.RevokeSession(ctx, claims.SessionID, claims.UserID)
}

var (
	ErrInvalidCredentials = errors.New("邮箱或密码不正确")
	ErrUnauthenticated    = errors.New("登录状态无效或已过期")
	ErrForbidden          = errors.New("当前账号无权访问该团队")
	ErrAuthBackend        = errors.New("认证数据服务暂不可用")
)

func validateCredentials(name, email, password string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("姓名不能为空")
	}
	if strings.TrimSpace(email) == "" || !strings.Contains(email, "@") {
		return errors.New("请输入有效邮箱地址")
	}
	if len(password) < 10 {
		return errors.New("密码至少需要 10 个字符")
	}
	return nil
}

func (s AuthService) issue(
	ctx context.Context,
	user domain.User,
) (LoginResult, error) {
	sessionID, err := randomSessionID()
	if err != nil {
		return LoginResult{}, err
	}
	expires := time.Now().Add(14 * 24 * time.Hour)
	if err = s.Store.CreateSession(ctx, sessionID, user.ID, expires); err != nil {
		return LoginResult{}, fmt.Errorf("%w: 创建登录会话失败: %v", ErrAuthBackend, err)
	}
	token, err := s.Signer.Issue(sessionID, user.ID, user.TeamID, expires)
	if err != nil {
		_ = s.Store.RevokeSession(ctx, sessionID, user.ID)
		return LoginResult{}, err
	}
	teamName := ""
	if teamStore, ok := s.Store.(interface {
		TeamName(context.Context, int64) (string, error)
	}); ok {
		teamName, _ = teamStore.TeamName(ctx, user.TeamID)
	}
	return LoginResult{
		AccessToken: token,
		TokenType:   "bearer",
		User: map[string]any{
			"id": user.ID, "email": user.Email, "name": user.Name,
			"role": user.Role, "team_id": user.TeamID,
			"team_name": teamName,
		},
	}, nil
}

func randomSessionID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(bytes[:4]) + "-" +
		hex.EncodeToString(bytes[4:6]) + "-" +
		hex.EncodeToString(bytes[6:8]) + "-" +
		hex.EncodeToString(bytes[8:10]) + "-" +
		hex.EncodeToString(bytes[10:]), nil
}

var _ AuthStore = (*repository.Postgres)(nil)
