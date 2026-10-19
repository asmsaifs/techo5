package remote

import (
	"testing"
	"time"
)

const testKey = "0123456789abcdef0123456789abcdef0123456789abcdef"

func TestSigning(t *testing.T) {
	secret := SecretOf(testKey)
	now := time.Now()
	body := []byte(`{"state":true}`)
	h := Sign(testKey, "POST", "/api/v1/entities/x", body, now)
	if err := verify(secret, h, "POST", "/api/v1/entities/x", body, now); err != nil {
		t.Fatalf("a good request: %v", err)
	}
	if err := verify(secret, h, "POST", "/api/v1/entities/x", body, now); err != errReplayed {
		t.Errorf("the same request again: %v", err)
	}
	for name, err := range map[string]error{
		"another key":   verify(SecretOf("other"), Sign(testKey, "GET", "/api/v1/device", nil, now), "GET", "/api/v1/device", nil, now),
		"another path":  verify(secret, Sign(testKey, "GET", "/api/v1/device", nil, now), "GET", "/api/v1/entities", nil, now),
		"another verb":  verify(secret, Sign(testKey, "GET", "/api/v1/device", nil, now), "POST", "/api/v1/device", nil, now),
		"another body":  verify(secret, Sign(testKey, "POST", "/p", []byte("a"), now), "POST", "/p", []byte("b"), now),
		"unsigned":      verify(secret, "", "GET", "/api/v1/device", nil, now),
		"garbage":       verify(secret, "v1 x y z", "GET", "/api/v1/device", nil, now),
		"another query": verify(secret, Sign(testKey, "GET", "/api/v1/logs?a=1", nil, now), "GET", "/api/v1/logs?a=2", nil, now),
	} {
		if err != errUnsigned {
			t.Errorf("%s: %v, want not signed", name, err)
		}
	}
	if err := verify(secret, Sign(testKey, "GET", "/p", nil, now.Add(-time.Minute)), "GET", "/p", nil, now); err != errStale {
		t.Errorf("an old request: %v", err)
	}
	if err := verify("", Sign(testKey, "GET", "/p", nil, now), "GET", "/p", nil, now); err != errNoKey {
		t.Errorf("no key made: %v", err)
	}
}

func TestTheKeyIsNotKept(t *testing.T) {
	key := NewKey()
	if len(key) < 40 || SecretOf(key) == key {
		t.Errorf("key %q, secret %q", key, SecretOf(key))
	}
	if NewKey() == key {
		t.Error("two keys are the same")
	}
}

// Failures turn away the address making them, and nobody else.
func TestFailuresTurnAwayOnlyTheirAddress(t *testing.T) {
	now := time.Now()
	for range failures {
		failed("192.0.2.66", now)
	}
	if !turnedAway("192.0.2.66", now) {
		t.Error("an address that failed again and again was not turned away")
	}
	if turnedAway("192.0.2.10", now) {
		t.Error("another address was turned away for it")
	}
	if turnedAway("192.0.2.66", now.Add(failWindow)) {
		t.Error("still turned away after the window")
	}
}
