package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
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

	return token, nil
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
