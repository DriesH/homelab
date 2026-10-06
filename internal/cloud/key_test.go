package cloud

import (
	"errors"
	"strings"
	"testing"
)

func TestRecoveryKeyRoundTrip(t *testing.T) {
	key, err := NewRecoveryKey()
	if err != nil {
		t.Fatal(err)
	}

	text := key.String()
	if !strings.HasPrefix(text, "HLC1-") || len(strings.Split(text, "-")) != 15 {
		t.Fatalf("key = %q", text)
	}

	lookAlikes := strings.NewReplacer("O", "0", "I", "1", "B", "8").Replace(text)
	for _, typed := range []string{text, strings.ToLower(text), strings.ReplaceAll(text, "-", " "), strings.ReplaceAll(text, "-", ""), lookAlikes} {
		parsed, err := ParseRecoveryKey(typed)
		if err != nil || parsed != key {
			t.Errorf("parse %q: %v", typed, err)
		}
	}
}

func TestRecoveryKeyCatchesTypos(t *testing.T) {
	key, _ := NewRecoveryKey()
	text := key.String()

	// Change one character in the middle.
	index := len(text) / 2
	replacement := byte('A')
	if text[index] == 'A' {
		replacement = 'B'
	}
	typo := text[:index] + string(replacement) + text[index+1:]

	for _, typed := range []string{typo, text[:len(text)-1], "HLC2" + text[4:], "", "hello"} {
		if _, err := ParseRecoveryKey(typed); !errors.Is(err, ErrInvalidKey) {
			t.Errorf("parse %q: err = %v", typed, err)
		}
	}
}

func TestRecoveryKeyPasswords(t *testing.T) {
	key, _ := NewRecoveryKey()
	password, salt := key.CryptPasswords()
	if len(password) != 22 || len(salt) != 22 || password == salt {
		t.Errorf("password %q, salt %q", password, salt)
	}
}

func TestRecoveryKeyEndsWith(t *testing.T) {
	key, _ := NewRecoveryKey()
	groups := strings.Split(key.String(), "-")
	last := groups[len(groups)-2] + "-" + groups[len(groups)-1]

	if !key.endsWith(last) || !key.endsWith(strings.ToLower(last)) || !key.endsWith(strings.ReplaceAll(last, "-", " ")) {
		t.Errorf("%q did not match", last)
	}
	if key.endsWith(groups[1]+"-"+groups[2]) || key.endsWith("") {
		t.Error("wrong groups matched")
	}
}
