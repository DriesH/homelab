package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RFC 6238 defaults, which every authenticator app supports.
const (
	totpPeriod = 30
	totpDigits = 6
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

func GenerateTOTPSecret() (string, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}

	return base32NoPadding.EncodeToString(secret), nil
}

func TOTPURI(issuer, account, secret string) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{
		"secret": {secret},
		"issuer": {issuer},
		"digits": {fmt.Sprint(totpDigits)},
		"period": {fmt.Sprint(totpPeriod)},
	}

	return "otpauth://totp/" + label + "?" + query.Encode()
}

func VerifyTOTP(secret, code string) bool {
	_, ok := matchTOTP(secret, code, time.Now())

	return ok
}

// matchTOTP returns the time step the code belongs to, allowing one step of clock drift.
func matchTOTP(secret, code string, now time.Time) (int64, bool) {
	key, err := base32NoPadding.DecodeString(strings.ToUpper(strings.TrimRight(secret, "=")))
	if err != nil || len(code) != totpDigits {
		return 0, false
	}

	current := now.Unix() / totpPeriod
	for _, step := range []int64{current - 1, current, current + 1} {
		if subtle.ConstantTimeCompare([]byte(totpCode(key, step)), []byte(code)) == 1 {
			return step, true
		}
	}

	return 0, false
}

func totpCode(key []byte, step int64) string {
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, uint64(step))

	mac := hmac.New(sha1.New, key)
	mac.Write(message)
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", totpDigits, value%1_000_000)
}
