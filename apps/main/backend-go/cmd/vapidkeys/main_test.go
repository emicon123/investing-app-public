package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestRun_PrintsTwoEnvLines(t *testing.T) {
	var out bytes.Buffer
	if err := run(&out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "VAPID_PUBLIC_KEY=") || !strings.HasPrefix(lines[1], "VAPID_PRIVATE_KEY=") {
		t.Fatalf("unexpected output shape (%d lines)", len(lines))
	}
	pub, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(lines[0], "VAPID_PUBLIC_KEY="))
	if err != nil || len(pub) != 65 || pub[0] != 0x04 {
		t.Fatalf("public key is not an uncompressed P-256 point (len %d, err %v)", len(pub), err)
	}
	priv, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(lines[1], "VAPID_PRIVATE_KEY="))
	if err != nil || len(priv) == 0 || len(priv) > 32 {
		t.Fatalf("private key has the wrong shape (len %d, err %v)", len(priv), err)
	}
}

func TestRun_GeneratorError(t *testing.T) {
	orig := generate
	t.Cleanup(func() { generate = orig })
	generate = func() (string, string, error) { return "", "", errors.New("no entropy") }
	var out bytes.Buffer
	if err := run(&out); err == nil || out.Len() != 0 {
		t.Fatalf("expected an error and no output, got %v and %d bytes", err, out.Len())
	}
}
