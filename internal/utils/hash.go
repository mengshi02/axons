// Package utils provides common utility functions used across the application.
package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
)

// ComputeSHA256 computes a SHA256 hash of content and returns the hex-encoded string.
func ComputeSHA256(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

// ComputeMD5 computes an MD5 hash of content and returns the hex-encoded string.
func ComputeMD5(content []byte) string {
	hash := md5.Sum(content)
	return hex.EncodeToString(hash[:])
}