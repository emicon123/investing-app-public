// Command vapidkeys prints a fresh VAPID key pair for Web Push (ADR-108 §7) as two .env lines:
//
//	cd apps/main/backend-go && go run ./cmd/vapidkeys
//
// Copy both lines, plus VAPID_SUBJECT=mailto:<you>, into infra/.env by hand. The command never
// writes a file. Rotating the pair invalidates every stored browser subscription.
package main

import (
	"fmt"
	"io"
	"os"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// generate is a variable so the error path can be tested.
var generate = webpush.GenerateVAPIDKeys

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "vapidkeys:", err)
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	priv, pub, err := generate()
	if err != nil {
		return fmt.Errorf("generate VAPID keys: %w", err)
	}
	_, err = fmt.Fprintf(w, "VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\n", pub, priv)
	return err
}
