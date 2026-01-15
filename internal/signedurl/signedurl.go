// Package signedurl provides HMAC-signed URL generation and verification
// for time-limited access to storage objects.
package signedurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var (
	ErrExpired       = errors.New("signed URL has expired")
	ErrInvalidSig    = errors.New("invalid signature")
	ErrMissingParams = errors.New("missing required parameters")
)

// Signer creates and verifies signed URLs
type Signer struct {
	secret []byte
}

// NewSigner creates a new signer with the given secret
func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret)}
}

// SignedURLParams holds parameters for signed URL generation
type SignedURLParams struct {
	Bucket       string
	Key          string
	ExpiresIn    time.Duration // How long until expiration
	MaxDownloads int           // 0 = unlimited
}

// SignedURL represents a generated signed URL
type SignedURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
	Signature string    `json:"signature"`
}

// Generate creates a signed URL for accessing a storage object
func (s *Signer) Generate(baseURL string, params SignedURLParams) (*SignedURL, error) {
	if params.Bucket == "" || params.Key == "" {
		return nil, ErrMissingParams
	}

	expiresAt := time.Now().Add(params.ExpiresIn)
	expTimestamp := expiresAt.Unix()

	// Create the string to sign: bucket/key:expires[:max_downloads]
	toSign := fmt.Sprintf("%s/%s:%d", params.Bucket, params.Key, expTimestamp)
	if params.MaxDownloads > 0 {
		toSign = fmt.Sprintf("%s:%d", toSign, params.MaxDownloads)
	}

	// Create HMAC signature
	sig := s.sign(toSign)

	// Build URL with query parameters
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	// Path: /s/{bucket}/{key}
	u.Path = fmt.Sprintf("/s/%s/%s", params.Bucket, params.Key)

	q := u.Query()
	q.Set("exp", strconv.FormatInt(expTimestamp, 10))
	q.Set("sig", sig)
	if params.MaxDownloads > 0 {
		q.Set("max", strconv.Itoa(params.MaxDownloads))
	}
	u.RawQuery = q.Encode()

	return &SignedURL{
		URL:       u.String(),
		ExpiresAt: expiresAt,
		Signature: sig,
	}, nil
}

// VerifyParams holds extracted parameters from a signed URL
type VerifyParams struct {
	Bucket       string
	Key          string
	ExpiresAt    time.Time
	MaxDownloads int
}

// Verify checks if a signed URL is valid and not expired
func (s *Signer) Verify(bucket, key, signature, expStr, maxStr string) (*VerifyParams, error) {
	if bucket == "" || key == "" || signature == "" || expStr == "" {
		return nil, ErrMissingParams
	}

	// Parse expiration
	expTimestamp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil {
		return nil, ErrMissingParams
	}
	expiresAt := time.Unix(expTimestamp, 0)

	// Check expiration
	if time.Now().After(expiresAt) {
		return nil, ErrExpired
	}

	// Parse max downloads
	maxDownloads := 0
	if maxStr != "" {
		maxDownloads, _ = strconv.Atoi(maxStr)
	}

	// Recreate the string that was signed
	toSign := fmt.Sprintf("%s/%s:%d", bucket, key, expTimestamp)
	if maxDownloads > 0 {
		toSign = fmt.Sprintf("%s:%d", toSign, maxDownloads)
	}

	// Verify signature
	expectedSig := s.sign(toSign)
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return nil, ErrInvalidSig
	}

	return &VerifyParams{
		Bucket:       bucket,
		Key:          key,
		ExpiresAt:    expiresAt,
		MaxDownloads: maxDownloads,
	}, nil
}

// sign creates an HMAC-SHA256 signature and returns base64url encoding
func (s *Signer) sign(data string) string {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(data))
	sig := h.Sum(nil)
	// Use URL-safe base64 without padding
	return strings.TrimRight(base64.URLEncoding.EncodeToString(sig), "=")
}
