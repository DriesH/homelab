package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	SessionTTL    = 12 * time.Hour
	maxFailures   = 5
	lockoutWindow = 15 * time.Minute
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLockedOut          = errors.New("too many failed attempts, try again later")
)

type Service struct {
	admin Admin
	now   func() time.Time
	// path is where sessions are saved, so a restart doesn't sign you out. Empty means memory only.
	path string

	mu           sync.Mutex
	sessions     map[[32]byte]time.Time
	failures     map[string]*failure
	lastTOTPStep int64
}

type failure struct {
	count int
	first time.Time
}

func NewService(admin Admin) *Service {
	return &Service{
		admin:    admin,
		now:      time.Now,
		sessions: map[[32]byte]time.Time{},
		failures: map[string]*failure{},
	}
}

type savedSessions struct {
	// Sessions maps the SHA-256 of each token to its expiry. The tokens themselves are never saved.
	Sessions     map[string]time.Time `json:"sessions"`
	LastTOTPStep int64                `json:"lastTotpStep"`
}

// PersistTo loads saved sessions from path and saves them there from now on.
func (s *Service) PersistTo(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.path = path

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var saved savedSessions
	if err := json.Unmarshal(data, &saved); err != nil {
		return err
	}

	for hash, expiresAt := range saved.Sessions {
		var key [32]byte
		if decoded, err := hex.DecodeString(hash); err == nil && len(decoded) == len(key) && s.now().Before(expiresAt) {
			copy(key[:], decoded)
			s.sessions[key] = expiresAt
		}
	}
	s.lastTOTPStep = max(s.lastTOTPStep, saved.LastTOTPStep)

	return nil
}

// saveLocked writes the sessions with 0600. It needs s.mu.
func (s *Service) saveLocked() error {
	if s.path == "" {
		return nil
	}

	saved := savedSessions{Sessions: map[string]time.Time{}, LastTOTPStep: s.lastTOTPStep}
	for key, expiresAt := range s.sessions {
		if s.now().Before(expiresAt) {
			saved.Sessions[hex.EncodeToString(key[:])] = expiresAt
		}
	}

	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}

	temp := filepath.Join(filepath.Dir(s.path), "."+filepath.Base(s.path)+".tmp")
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}

	return os.Rename(temp, s.path)
}

// Login returns a session token. clientIP is used for lockout after repeated failures.
func (s *Service) Login(username, password, code, clientIP string) (string, error) {
	if s.isLockedOut(clientIP) {
		return "", ErrLockedOut
	}

	// Always hash, so a wrong username takes as long as a wrong password.
	passwordOK, err := VerifyPassword(password, s.admin.PasswordHash)
	if err != nil {
		return "", err
	}
	usernameOK := subtle.ConstantTimeCompare([]byte(username), []byte(s.admin.Username)) == 1
	step, codeOK := matchTOTP(s.admin.TOTPSecret, code, s.now())

	s.mu.Lock()
	defer s.mu.Unlock()

	// A code can only be used once.
	if codeOK && step <= s.lastTOTPStep {
		codeOK = false
	}

	if !usernameOK || !passwordOK || !codeOK {
		s.recordFailure(clientIP)
		return "", ErrInvalidCredentials
	}

	delete(s.failures, clientIP)
	s.lastTOTPStep = step

	token, err := randomToken()
	if err != nil {
		return "", err
	}
	s.sessions[sha256.Sum256([]byte(token))] = s.now().Add(SessionTTL)
	if err := s.saveLocked(); err != nil {
		return "", err
	}

	return token, nil
}

// CheckPassword asks for the password again before a sensitive action. Wrong
// passwords count toward the same lockout as logins.
func (s *Service) CheckPassword(password, clientIP string) error {
	if s.isLockedOut(clientIP) {
		return ErrLockedOut
	}

	ok, err := VerifyPassword(password, s.admin.PasswordHash)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !ok {
		s.recordFailure(clientIP)
		return ErrInvalidCredentials
	}

	return nil
}

// CheckPasswordAndCode asks for the password and an authenticator code again,
// before the most sensitive actions. Failures count toward the login lockout,
// and a code works only once, like at login.
func (s *Service) CheckPasswordAndCode(password, code, clientIP string) error {
	if s.isLockedOut(clientIP) {
		return ErrLockedOut
	}

	passwordOK, err := VerifyPassword(password, s.admin.PasswordHash)
	if err != nil {
		return err
	}
	step, codeOK := matchTOTP(s.admin.TOTPSecret, code, s.now())

	s.mu.Lock()
	defer s.mu.Unlock()

	if codeOK && step <= s.lastTOTPStep {
		codeOK = false
	}
	if !passwordOK || !codeOK {
		s.recordFailure(clientIP)
		return ErrInvalidCredentials
	}

	s.lastTOTPStep = step
	return s.saveLocked()
}

func (s *Service) Validate(token string) bool {
	key := sha256.Sum256([]byte(token))

	s.mu.Lock()
	defer s.mu.Unlock()

	expiresAt, ok := s.sessions[key]
	if !ok {
		return false
	}
	if s.now().After(expiresAt) {
		delete(s.sessions, key)
		return false
	}

	return true
}

func (s *Service) Logout(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, sha256.Sum256([]byte(token)))
	// A failed save only means this session could come back after a restart, until it expires.
	s.saveLocked()
}

func (s *Service) Username() string {
	return s.admin.Username
}

func (s *Service) isLockedOut(clientIP string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.failures[clientIP]
	if !ok {
		return false
	}
	if s.now().Sub(entry.first) > lockoutWindow {
		delete(s.failures, clientIP)
		return false
	}

	return entry.count >= maxFailures
}

func (s *Service) recordFailure(clientIP string) {
	entry, ok := s.failures[clientIP]
	if !ok || s.now().Sub(entry.first) > lockoutWindow {
		s.failures[clientIP] = &failure{count: 1, first: s.now()}
		return
	}

	entry.count++
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
