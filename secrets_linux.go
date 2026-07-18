//go:build linux

package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
)

func linuxSecretKey() ([]byte, error) {
	base, err := appDataDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(base, "secrets.key")
	if data, readErr := os.ReadFile(path); readErr == nil {
		if len(data) != 32 {
			return nil, errors.New("Linux secret key has an invalid length")
		}
		return data, nil
	} else if !os.IsNotExist(readErr) {
		return nil, readErr
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return linuxSecretKey()
		}
		return nil, err
	}
	_, writeErr := file.Write(key)
	closeErr := file.Close()
	if writeErr != nil {
		return nil, writeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return key, nil
}

func linuxSecretAEAD() (cipher.AEAD, error) {
	key, err := linuxSecretKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func protectSecret(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	aead, err := linuxSecretAEAD()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nil, nonce, []byte(value), []byte("palserver-launcher"))
	payload := append(nonce, sealed...)
	return base64.RawStdEncoding.EncodeToString(payload), nil
}

func unprotectSecret(value string) (string, error) {
	if value == "" {
		return "", errors.New("secret is not configured")
	}
	payload, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil {
		return "", err
	}
	aead, err := linuxSecretAEAD()
	if err != nil {
		return "", err
	}
	if len(payload) < aead.NonceSize() {
		return "", errors.New("encrypted secret is truncated")
	}
	plaintext, err := aead.Open(nil, payload[:aead.NonceSize()], payload[aead.NonceSize():], []byte("palserver-launcher"))
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
