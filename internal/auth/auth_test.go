package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// RFC 6238 appendix B test key: ASCII "12345678901234567890".
var rfcSecret = base32NoPadding.EncodeToString([]byte("12345678901234567890"))

func TestTOTPMatchesRFCVectors(t *testing.T) {
	cases := map[int64]string{
		59:         "287082",
		1111111109: "081804",
		1234567890: "005924",
		2000000000: "279037",
	}

	for unix, code := range cases {
		if _, ok := matchTOTP(rfcSecret, code, time.Unix(unix, 0)); !ok {
			t.Errorf("code %s not accepted at %d", code, unix)
		}
	}
}

func TestTOTPRejectsCodeOutsideDriftWindow(t *testing.T) {
	if _, ok := matchTOTP(rfcSecret, "287082", time.Unix(59+3*totpPeriod, 0)); ok {
		t.Fatal("old code accepted")
	}
}

func TestPasswordRoundTrip(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}

	if ok, _ := VerifyPassword("correct horse", hash); !ok {
		t.Error("correct password rejected")
	}
	if ok, _ := VerifyPassword("wrong horse", hash); ok {
		t.Error("wrong password accepted")
	}
}

func newTestService(t *testing.T, now time.Time) *Service {
	t.Helper()

	hash, err := HashPassword("secret")
	if err != nil {
		t.Fatal(err)
	}

	service := NewService(Admin{Username: "admin", PasswordHash: hash, TOTPSecret: rfcSecret})
	service.now = func() time.Time { return now }

	return service
}

func TestLoginCreatesValidSession(t *testing.T) {
	service := newTestService(t, time.Unix(59, 0))

	token, err := service.Login("admin", "secret", "287082", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}
	if !service.Validate(token) {
		t.Fatal("session not valid")
	}

	service.Logout(token)
	if service.Validate(token) {
		t.Fatal("session valid after logout")
	}
}

func TestLoginRejectsReusedCode(t *testing.T) {
	service := newTestService(t, time.Unix(59, 0))

	if _, err := service.Login("admin", "secret", "287082", "10.0.0.2"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login("admin", "secret", "287082", "10.0.0.2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLoginLocksOutAfterRepeatedFailures(t *testing.T) {
	service := newTestService(t, time.Unix(59, 0))

	for range maxFailures {
		service.Login("admin", "wrong", "287082", "10.0.0.2")
	}

	if _, err := service.Login("admin", "secret", "287082", "10.0.0.2"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("expected ErrLockedOut, got %v", err)
	}
	if _, err := service.Login("admin", "secret", "287082", "10.0.0.3"); err != nil {
		t.Fatalf("other IP should not be locked out: %v", err)
	}
}

func TestSessionsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	service := newTestService(t, time.Unix(59, 0))
	if err := service.PersistTo(path); err != nil {
		t.Fatal(err)
	}

	token, err := service.Login("admin", "secret", "287082", "10.0.0.2")
	if err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), token) {
		t.Fatal("the token itself was saved")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions file mode = %v", info.Mode().Perm())
	}

	restarted := newTestService(t, time.Unix(59, 0))
	if err := restarted.PersistTo(path); err != nil {
		t.Fatal(err)
	}
	if !restarted.Validate(token) {
		t.Fatal("session lost after restart")
	}
	// The used code stays used after a restart.
	if _, err := restarted.Login("admin", "secret", "287082", "10.0.0.2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("reused code after restart: err = %v", err)
	}

	restarted.Logout(token)
	again := newTestService(t, time.Unix(59, 0))
	again.PersistTo(path)
	if again.Validate(token) {
		t.Fatal("session valid after logout and restart")
	}

	expired := newTestService(t, time.Unix(59, 0).Add(SessionTTL+time.Minute))
	expired.PersistTo(path)
	if len(expired.sessions) != 0 {
		t.Fatal("expired sessions were loaded")
	}
}

func TestCheckPasswordAndCode(t *testing.T) {
	service := newTestService(t, time.Unix(59, 0))

	if err := service.CheckPasswordAndCode("secret", "000000", "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong code: %v", err)
	}
	if err := service.CheckPasswordAndCode("wrong", "287082", "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong password: %v", err)
	}
	if err := service.CheckPasswordAndCode("secret", "287082", "10.0.0.1"); err != nil {
		t.Fatalf("right password and code: %v", err)
	}
	if err := service.CheckPasswordAndCode("secret", "287082", "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("reused code: %v", err)
	}
	if _, err := service.Login("admin", "secret", "287082", "10.0.0.2"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("login with a code that was used for a check: %v", err)
	}

	for range maxFailures {
		service.CheckPasswordAndCode("wrong", "000000", "10.0.0.3")
	}
	if _, err := service.Login("admin", "secret", "287082", "10.0.0.3"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("login after failed checks: %v", err)
	}
}

func TestCheckPasswordCountsFailures(t *testing.T) {
	service := newTestService(t, time.Unix(59, 0))

	if err := service.CheckPassword("secret", "10.0.0.1"); err != nil {
		t.Fatalf("right password: %v", err)
	}
	for range maxFailures {
		if err := service.CheckPassword("wrong", "10.0.0.1"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("wrong password: %v", err)
		}
	}
	if err := service.CheckPassword("secret", "10.0.0.1"); !errors.Is(err, ErrLockedOut) {
		t.Fatalf("after failures: %v", err)
	}
}
