// cmd/decryptor — a small, on-demand CLI that reverses the fieldcrypto processor's
// encryption. It reuses the exact same crypto primitives and disk keystore as the
// processor (via the fieldcryptoprocessor package), so there is one implementation.
//
// Modes:
//
//	Single value:
//	  decryptor --key-dir /var/keys --key-id key-... --value <base64>
//
//	Log line (reads encryption.key_id from the record):
//	  decryptor --key-dir /var/keys --input line.json --fields user.document,user.card
//
// The input file for --input mode is a single JSON object of flat key/value pairs
// (an exported log record's attributes), e.g.:
//
//	{ "encryption.key_id": "key-...", "user.document": "<base64>", "user.card": "<base64>" }
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	fc "github.com/aborigene/custom-otel-collector-bindplane/fieldcryptoprocessor"
)

const keyIDAttr = "encryption.key_id"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "decryptor: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	var (
		keyDir = flag.String("key-dir", "/var/keys", "keystore directory (contains keystore.json)")
		keyID  = flag.String("key-id", "", "key id to use (single-value mode)")
		value  = flag.String("value", "", "base64 ciphertext to decrypt (single-value mode)")
		input  = flag.String("input", "", "path to a JSON log record (log-line mode)")
		fields = flag.String("fields", "", "comma-separated field names to decrypt (log-line mode)")
	)
	flag.Parse()

	ctx := context.Background()
	kp, err := fc.NewDiskKeyProvider(*keyDir)
	if err != nil {
		return fmt.Errorf("open keystore at %s: %w", *keyDir, err)
	}

	switch {
	case *value != "":
		if *keyID == "" {
			return fmt.Errorf("--value requires --key-id")
		}
		pt, err := decryptOne(ctx, kp, *keyID, *value)
		if err != nil {
			return err
		}
		fmt.Println(pt)
		return nil

	case *input != "":
		if *fields == "" {
			return fmt.Errorf("--input requires --fields")
		}
		return decryptLine(ctx, kp, *input, strings.Split(*fields, ","))

	default:
		flag.Usage()
		return fmt.Errorf("choose a mode: --value (single) or --input+--fields (log line)")
	}
}

func decryptOne(ctx context.Context, kp fc.KeyProvider, keyID, b64 string) (string, error) {
	key, err := kp.Key(ctx, keyID)
	if err != nil {
		return "", fmt.Errorf("key id %q not in keystore: %w", keyID, err)
	}
	pt, err := fc.DecryptAESGCM(key, b64)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}

func decryptLine(ctx context.Context, kp fc.KeyProvider, path string, fields []string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return fmt.Errorf("parse %s as a JSON object: %w", path, err)
	}
	keyID, _ := record[keyIDAttr].(string)
	if keyID == "" {
		return fmt.Errorf("record has no %q; nothing to decrypt", keyIDAttr)
	}
	for _, f := range fields {
		f = strings.TrimSpace(f)
		b64, ok := record[f].(string)
		if !ok {
			fmt.Printf("%s: (absent)\n", f)
			continue
		}
		pt, err := decryptOne(ctx, kp, keyID, b64)
		if err != nil {
			return fmt.Errorf("field %q: %w", f, err)
		}
		fmt.Printf("%s: %s\n", f, pt)
	}
	return nil
}
