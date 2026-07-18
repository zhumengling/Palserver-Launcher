package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	agentPasswordMinimumLength = 10
	agentPasswordMaximumLength = 256
	agentCredentialVersion     = 1
	agentArgonTime             = 3
	agentArgonMemory           = 64 * 1024
	agentArgonThreads          = 2
	agentArgonKeyLength        = 32
)

type agentPasswordCredential struct {
	Version   int    `json:"version"`
	Algorithm string `json:"algorithm"`
	Time      uint32 `json:"time"`
	Memory    uint32 `json:"memory"`
	Threads   uint8  `json:"threads"`
	Salt      string `json:"salt"`
	Hash      string `json:"hash"`
}

type agentAuth struct {
	mu                sync.Mutex
	credentialPath    string
	credential        *agentPasswordCredential
	fixedPasswordHash *[sha256.Size]byte
	sessions          map[string]time.Time
	attempts          map[string]agentLoginAttempt
}

type agentLoginRequest struct {
	Password string `json:"password"`
}

type agentSetupRequest struct {
	Password string `json:"password"`
}

func randomHex(bytes int) (string, error) {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return hex.EncodeToString(value), nil
}

// newAgentAuth creates a preconfigured in-memory password authenticator for
// unit tests and explicitly preconfigured preview processes. Production Linux
// starts with newPersistentAgentAuth so the first browser visit creates the
// administrator password.
func newAgentAuth(password string) *agentAuth {
	digest := sha256.Sum256([]byte(password))
	return &agentAuth{fixedPasswordHash: &digest, sessions: map[string]time.Time{}, attempts: map[string]agentLoginAttempt{}}
}

func newPersistentAgentAuth(path string) (*agentAuth, error) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." || !filepath.IsAbs(path) {
		return nil, errors.New("administrator credential file must use an absolute path")
	}
	auth := &agentAuth{credentialPath: path, sessions: map[string]time.Time{}, attempts: map[string]agentLoginAttempt{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return auth, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read administrator credential: %w", err)
	}
	if info, statErr := os.Stat(path); statErr != nil {
		return nil, fmt.Errorf("stat administrator credential: %w", statErr)
	} else if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("administrator credential file is accessible by other users")
	}
	var credential agentPasswordCredential
	if err := json.Unmarshal(data, &credential); err != nil {
		return nil, fmt.Errorf("decode administrator credential: %w", err)
	}
	if err := validateAgentCredential(credential); err != nil {
		return nil, err
	}
	auth.credential = &credential
	return auth, nil
}

func validateAgentCredential(credential agentPasswordCredential) error {
	if credential.Version != agentCredentialVersion || credential.Algorithm != "argon2id" {
		return errors.New("administrator credential format is not supported")
	}
	if credential.Time < 1 || credential.Time > 10 || credential.Memory < 8*1024 || credential.Memory > 256*1024 || credential.Threads < 1 || credential.Threads > 8 {
		return errors.New("administrator credential parameters are invalid")
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(credential.Salt)
	hash, hashErr := base64.RawStdEncoding.DecodeString(credential.Hash)
	if saltErr != nil || hashErr != nil || len(salt) < 16 || len(salt) > 64 || len(hash) != agentArgonKeyLength {
		return errors.New("administrator credential data is invalid")
	}
	return nil
}

func validateAgentPassword(password string) error {
	if len(password) < agentPasswordMinimumLength {
		return fmt.Errorf("管理密码至少需要 %d 个字符", agentPasswordMinimumLength)
	}
	if len(password) > agentPasswordMaximumLength {
		return fmt.Errorf("管理密码不能超过 %d 个字符", agentPasswordMaximumLength)
	}
	if strings.TrimSpace(password) != password {
		return errors.New("管理密码首尾不能包含空格")
	}
	return nil
}

func createAgentCredential(password string) (agentPasswordCredential, error) {
	if err := validateAgentPassword(password); err != nil {
		return agentPasswordCredential{}, err
	}
	salt := make([]byte, 24)
	if _, err := rand.Read(salt); err != nil {
		return agentPasswordCredential{}, err
	}
	hash := argon2.IDKey([]byte(password), salt, agentArgonTime, agentArgonMemory, agentArgonThreads, agentArgonKeyLength)
	return agentPasswordCredential{
		Version: agentCredentialVersion, Algorithm: "argon2id", Time: agentArgonTime, Memory: agentArgonMemory, Threads: agentArgonThreads,
		Salt: base64.RawStdEncoding.EncodeToString(salt), Hash: base64.RawStdEncoding.EncodeToString(hash),
	}, nil
}

func (auth *agentAuth) setupRequired() bool {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	return auth.credential == nil && auth.fixedPasswordHash == nil
}

func (auth *agentAuth) setupPassword(password string) error {
	auth.mu.Lock()
	defer auth.mu.Unlock()
	if auth.credential != nil || auth.fixedPasswordHash != nil {
		return errors.New("管理密码已创建，不能重复初始化")
	}
	credential, err := createAgentCredential(password)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(credential, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(auth.credentialPath), 0o700); err != nil {
		return err
	}
	if err := replaceFileData(auth.credentialPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("保存管理密码失败：%w", err)
	}
	auth.credential = &credential
	return nil
}

func (auth *agentAuth) passwordMatches(password string) bool {
	auth.mu.Lock()
	credential := auth.credential
	fixed := auth.fixedPasswordHash
	auth.mu.Unlock()
	if fixed != nil {
		digest := sha256.Sum256([]byte(password))
		return subtle.ConstantTimeCompare(digest[:], fixed[:]) == 1
	}
	if credential == nil || validateAgentCredential(*credential) != nil {
		return false
	}
	salt, _ := base64.RawStdEncoding.DecodeString(credential.Salt)
	want, _ := base64.RawStdEncoding.DecodeString(credential.Hash)
	actual := argon2.IDKey([]byte(password), salt, credential.Time, credential.Memory, credential.Threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(actual, want) == 1
}

func (auth *agentAuth) createSession() (string, error) {
	session, err := randomHex(32)
	if err != nil {
		return "", err
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	now := time.Now()
	for id, expiry := range auth.sessions {
		if expiry.Before(now) {
			delete(auth.sessions, id)
		}
	}
	auth.sessions[session] = now.Add(24 * time.Hour)
	return session, nil
}

func setAgentSessionCookie(writer http.ResponseWriter, request *http.Request, session string) {
	http.SetCookie(writer, &http.Cookie{
		Name: agentSessionCookie, Value: session, Path: "/", HttpOnly: true,
		Secure: agentRequestSecure(request), SameSite: http.SameSiteStrictMode, MaxAge: 86400,
	})
}

func (auth *agentAuth) validSession(request *http.Request) bool {
	cookie, err := request.Cookie(agentSessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}
	auth.mu.Lock()
	defer auth.mu.Unlock()
	expiry, ok := auth.sessions[cookie.Value]
	if !ok || expiry.Before(time.Now()) {
		delete(auth.sessions, cookie.Value)
		return false
	}
	return true
}

func (auth *agentAuth) deleteSession(request *http.Request) {
	cookie, err := request.Cookie(agentSessionCookie)
	if err != nil {
		return
	}
	auth.mu.Lock()
	delete(auth.sessions, cookie.Value)
	auth.mu.Unlock()
}
