package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSessionExpired     = errors.New("session expired")
	ErrSessionRevoked     = errors.New("session revoked")
	ErrForbidden          = errors.New("forbidden")
)

type Role string

const (
	RoleVenueOperator           Role = "venue_operator"
	RoleLightingAssessor        Role = "lighting_assessor"
	RoleUrbanManagementReviewer Role = "urban_management_reviewer"
	RoleAdmin                   Role = "admin"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Role         Role   `json:"role"`
	Disabled     bool   `json:"disabled"`
}

// BootstrapUser is used only on first startup to provision an operator account.
// The plaintext password is never persisted; only the bcrypt hash is stored.
type BootstrapUser struct {
	ID       string
	Username string
	Password string
	Role     Role
}

type Session struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"token_hash"`
	ExpiresAt time.Time  `json:"expires_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type diskState struct {
	Users    []User    `json:"users"`
	Sessions []Session `json:"sessions"`
}

type Store struct {
	path string
	mu   sync.Mutex
	data diskState
}

func Open(path string, bootstrap ...BootstrapUser) (*Store, error) {
	s := &Store{path: path}
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(s.data.Users) == 0 && len(bootstrap) > 0 {
		seenIDs := make(map[string]struct{}, len(bootstrap))
		seenNames := make(map[string]struct{}, len(bootstrap))
		for _, seed := range bootstrap {
			if seed.ID == "" || seed.Username == "" || len(seed.Password) < 12 || !validRole(seed.Role) {
				return nil, fmt.Errorf("invalid bootstrap user %q: id, username, role and a 12-character password are required", seed.Username)
			}
			if _, exists := seenIDs[seed.ID]; exists {
				return nil, fmt.Errorf("duplicate bootstrap user id %q", seed.ID)
			}
			if _, exists := seenNames[seed.Username]; exists {
				return nil, fmt.Errorf("duplicate bootstrap username %q", seed.Username)
			}
			seenIDs[seed.ID] = struct{}{}
			seenNames[seed.Username] = struct{}{}
			hash, err := hashPassword(seed.Password)
			if err != nil {
				return nil, fmt.Errorf("hash bootstrap password for %s: %w", seed.Username, err)
			}
			s.data.Users = append(s.data.Users, User{ID: seed.ID, Username: seed.Username, PasswordHash: hash, Role: seed.Role})
		}
		if err := s.persist(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func validRole(role Role) bool {
	switch role {
	case RoleVenueOperator, RoleLightingAssessor, RoleUrbanManagementReviewer, RoleAdmin:
		return true
	default:
		return false
	}
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read auth store: %w", err)
	}
	if err := json.Unmarshal(data, &s.data); err != nil {
		return fmt.Errorf("decode auth store: %w", err)
	}
	return nil
}

func (s *Store) persist() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o750); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}
	data, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode auth store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write auth store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace auth store: %w", err)
	}
	return nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt password hash: %w", err)
	}
	return string(hash), nil
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (s *Store) Authenticate(username, password string) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, user := range s.data.Users {
		if user.Username == username && !user.Disabled && bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) == nil {
			return user, nil
		}
	}
	return User{}, ErrInvalidCredentials
}

func (s *Store) Login(username, password string, ttl time.Duration, now time.Time) (User, string, time.Time, error) {
	user, err := s.Authenticate(username, password)
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	token, err := newToken()
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	expires := now.Add(ttl)
	s.mu.Lock()
	s.data.Sessions = append(s.data.Sessions, Session{ID: token[:16], UserID: user.ID, TokenHash: tokenHash(token), ExpiresAt: expires})
	err = s.persist()
	s.mu.Unlock()
	if err != nil {
		return User{}, "", time.Time{}, err
	}
	return user, token, expires, nil
}

func (s *Store) Resolve(token string, now time.Time) (User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, session := range s.data.Sessions {
		if session.TokenHash != tokenHash(token) {
			continue
		}
		if session.RevokedAt != nil {
			return User{}, ErrSessionRevoked
		}
		if !now.Before(session.ExpiresAt) {
			return User{}, ErrSessionExpired
		}
		for _, user := range s.data.Users {
			if user.ID == session.UserID && !user.Disabled {
				return user, nil
			}
		}
		return User{}, ErrInvalidCredentials
	}
	return User{}, ErrInvalidCredentials
}

func (s *Store) Logout(token string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for idx := range s.data.Sessions {
		if s.data.Sessions[idx].TokenHash == tokenHash(token) {
			if s.data.Sessions[idx].RevokedAt == nil {
				s.data.Sessions[idx].RevokedAt = &now
			}
			return s.persist()
		}
	}
	return ErrInvalidCredentials
}

func RequireRole(user User, allowed ...Role) error {
	for _, role := range allowed {
		if user.Role == role {
			return nil
		}
	}
	return ErrForbidden
}
