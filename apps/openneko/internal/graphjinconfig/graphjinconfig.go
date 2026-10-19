// Package graphjinconfig writes the metadata GraphJin config with the
// setup-managed Postgres password. It runs as the openneko binary's
// graphjin-config role:
//
//	openneko graphjin-config CONFIG_JSON SECRET_KEY SEED_YAML OUTPUT_YAML
package graphjinconfig

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const encryptedPrefix = "enc:v1:"

var passwordLine = regexp.MustCompile(`(?m)^([\t ]*)password:.*$`)

type localConfig struct {
	PG struct {
		Password string `json:"password"`
	} `json:"pg"`
}

func decryptPassword(value, secret string) (string, error) {
	if !strings.HasPrefix(value, encryptedPrefix) {
		return value, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(value, encryptedPrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted password: %w", err)
	}
	if len(raw) < 28 {
		return "", errors.New("enc:v1 value is too short")
	}
	key := sha256.Sum256([]byte(strings.TrimSpace(secret)))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := raw[:12]
	tag := raw[12:28]
	ciphertextAndTag := append(append([]byte{}, raw[28:]...), tag...)
	plain, err := gcm.Open(nil, nonce, ciphertextAndTag, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt password: %w", err)
	}
	return string(plain), nil
}

func render(seed []byte, password string) ([]byte, error) {
	match := passwordLine.FindSubmatchIndex(seed)
	if match == nil {
		return nil, errors.New("seed config has no password field")
	}
	quoted, err := json.Marshal(password)
	if err != nil {
		return nil, err
	}
	replacement := append(append([]byte{}, seed[match[2]:match[3]]...), []byte("password: ")...)
	replacement = append(replacement, quoted...)
	out := make([]byte, 0, len(seed)+len(replacement))
	out = append(out, seed[:match[0]]...)
	out = append(out, replacement...)
	out = append(out, seed[match[1]:]...)
	return out, nil
}

// Main writes OUTPUT_YAML from args, which exclude the program name. It exits
// the process on error.
func Main(args []string) {
	if len(args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: openneko graphjin-config CONFIG_JSON SECRET_KEY SEED_YAML OUTPUT_YAML")
		os.Exit(2)
	}
	configPath, keyPath, seedPath, outputPath := args[0], args[1], args[2], args[3]
	password := os.Getenv("NEKO_PG_PASSWORD")
	if password == "" {
		password = "secret"
	}
	source := "NEKO_PG_PASSWORD env"

	if configBytes, err := os.ReadFile(configPath); err == nil {
		var config localConfig
		if err := json.Unmarshal(configBytes, &config); err != nil {
			fatal(fmt.Errorf("parse %s: %w", configPath, err))
		}
		if config.PG.Password != "" {
			secret := []byte{}
			if strings.HasPrefix(config.PG.Password, encryptedPrefix) {
				secret, err = os.ReadFile(keyPath)
				if err != nil {
					fatal(fmt.Errorf("read %s: %w", keyPath, err))
				}
			}
			password, err = decryptPassword(config.PG.Password, string(secret))
			if err != nil {
				fatal(err)
			}
			source = "config.json"
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		fatal(fmt.Errorf("read %s: %w", configPath, err))
	} else {
		fmt.Fprintf(os.Stderr, "[neko-graphjin] config.json missing; using %s\n", source)
	}

	seed, err := os.ReadFile(seedPath)
	if err != nil {
		fatal(err)
	}
	generated, err := render(seed, password)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		fatal(err)
	}
	temporary := fmt.Sprintf("%s.%d.tmp", outputPath, os.Getpid())
	if err := os.WriteFile(temporary, generated, 0o600); err != nil {
		fatal(err)
	}
	if err := os.Rename(temporary, outputPath); err != nil {
		_ = os.Remove(temporary)
		fatal(err)
	}
	if err := os.Chmod(outputPath, 0o600); err != nil {
		fatal(err)
	}
	fmt.Printf("[neko-graphjin] templated %s from %s\n", outputPath, source)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "[neko-graphjin]", err)
	os.Exit(1)
}
