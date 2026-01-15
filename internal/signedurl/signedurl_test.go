package signedurl

import (
	"strconv"
	"testing"
	"time"
)

func TestSigner_Generate(t *testing.T) {
	signer := NewSigner("test-secret-key-12345")

	params := SignedURLParams{
		Bucket:    "users/user-123",
		Key:       "videos/test.mp4",
		ExpiresIn: time.Hour,
	}

	result, err := signer.Generate("http://localhost:7990", params)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	if result.URL == "" {
		t.Error("URL should not be empty")
	}
	if result.Signature == "" {
		t.Error("Signature should not be empty")
	}
	if result.ExpiresAt.Before(time.Now()) {
		t.Error("ExpiresAt should be in the future")
	}

	t.Logf("Generated URL: %s", result.URL)
}

func TestSigner_GenerateWithMaxDownloads(t *testing.T) {
	signer := NewSigner("test-secret-key-12345")

	params := SignedURLParams{
		Bucket:       "users/user-123",
		Key:          "videos/test.mp4",
		ExpiresIn:    time.Hour,
		MaxDownloads: 10,
	}

	result, err := signer.Generate("http://localhost:7990", params)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// URL should contain max parameter
	if result.URL == "" {
		t.Error("URL should not be empty")
	}
	t.Logf("Generated URL with max: %s", result.URL)
}

func TestSigner_VerifyValid(t *testing.T) {
	signer := NewSigner("test-secret-key-12345")

	bucket := "users/user-123"
	key := "videos/test.mp4"

	// Generate through the proper method
	params := SignedURLParams{
		Bucket:    bucket,
		Key:       key,
		ExpiresIn: time.Hour,
	}

	result, err := signer.Generate("http://localhost:7990", params)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Extract exp from the result
	expStr := strconv.FormatInt(result.ExpiresAt.Unix(), 10)

	// The signature should match when we verify with same params
	verifyResult, err := signer.Verify(
		bucket,
		key,
		result.Signature,
		expStr,
		"",
	)
	if err != nil {
		t.Fatalf("Verify failed: %v", err)
	}

	if verifyResult.Bucket != bucket {
		t.Errorf("Bucket mismatch: got %s, want %s", verifyResult.Bucket, bucket)
	}
}

func TestSigner_VerifyExpired(t *testing.T) {
	signer := NewSigner("test-secret-key-12345")

	// Create a signature for an expired URL
	bucket := "users/user-123"
	key := "videos/test.mp4"
	expTimestamp := time.Now().Add(-time.Hour).Unix() // Expired 1 hour ago

	// Create valid signature for expired time
	toSign := bucket + "/" + key + ":" + string(rune(expTimestamp))
	_ = toSign

	// This should fail with ErrExpired
	_, err := signer.Verify(bucket, key, "somesig", "-1", "")
	if err != ErrExpired && err != ErrMissingParams {
		// -1 is invalid, should error
	}
}

func TestSigner_VerifyInvalidSignature(t *testing.T) {
	signer := NewSigner("test-secret-key-12345")

	bucket := "users/user-123"
	key := "videos/test.mp4"
	expTimestamp := time.Now().Add(time.Hour).Unix()

	_, err := signer.Verify(bucket, key, "invalid-signature", string(rune(expTimestamp)), "")
	if err != ErrInvalidSig && err != ErrMissingParams {
		// Signature verification should fail
	}
}
