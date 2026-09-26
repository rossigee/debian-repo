package reconcile

import (
	"testing"
	"time"

	minios3 "github.com/minio/minio-go/v7"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/validate"
)

func TestSidecarIsFresh(t *testing.T) {
	tests := []struct {
		name     string
		sidecar  *model.ChecksumSidecarV1
		stat     minios3.ObjectInfo
		expected bool
	}{
		{
			name:     "nil sidecar is not fresh",
			sidecar:  nil,
			stat:     minios3.ObjectInfo{ETag: "abc123"},
			expected: false,
		},
		{
			name: "matching ETag is fresh",
			sidecar: &model.ChecksumSidecarV1{
				ETag: "abc123",
			},
			stat:     minios3.ObjectInfo{ETag: "abc123"},
			expected: true,
		},
		{
			name: "mismatched ETag is not fresh",
			sidecar: &model.ChecksumSidecarV1{
				ETag: "abc123",
			},
			stat:     minios3.ObjectInfo{ETag: "xyz789"},
			expected: false,
		},
		{
			name: "empty ETags match (both empty)",
			sidecar: &model.ChecksumSidecarV1{
				ETag: "",
			},
			stat:     minios3.ObjectInfo{ETag: ""},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sidecarIsFresh(tt.sidecar, tt.stat)
			if result != tt.expected {
				t.Errorf("sidecarIsFresh() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildPackageVersionFromDebInfo(t *testing.T) {
	debInfo := &validate.DebInfo{
		Package:      "test-pkg",
		Version:      "1.0.0",
		Architecture: "amd64",
		MD5:          "abc123",
		SHA1:         "def456",
		SHA256:       "ghi789",
		Size:         1024,
		ControlFields: map[string]string{
			"Depends":    "libc6",
			"Section":    "main",
			"Priority":   "optional",
			"Maintainer": "test@example.com",
		},
	}

	stat := minios3.ObjectInfo{
		LastModified: time.Now(),
	}

	ver := buildPackageVersionFromDebInfo(debInfo, "pool/main/test-pkg_1.0.0_amd64.deb", stat)

	if ver.Version != "1.0.0" {
		t.Errorf("Version: got %s, want %s", ver.Version, "1.0.0")
	}
	if ver.Architecture != "amd64" {
		t.Errorf("Architecture: got %s, want %s", ver.Architecture, "amd64")
	}
	if ver.Checksums.MD5 != "abc123" {
		t.Errorf("MD5: got %s, want %s", ver.Checksums.MD5, "abc123")
	}
	if ver.Checksums.SHA1 != "def456" {
		t.Errorf("SHA1: got %s, want %s", ver.Checksums.SHA1, "def456")
	}
	if ver.Checksums.SHA256 != "ghi789" {
		t.Errorf("SHA256: got %s, want %s", ver.Checksums.SHA256, "ghi789")
	}
	if ver.Checksums.Size != 1024 {
		t.Errorf("Size: got %d, want %d", ver.Checksums.Size, 1024)
	}
	if len(ver.ControlFields) != len(debInfo.ControlFields) {
		t.Errorf("ControlFields length: got %d, want %d", len(ver.ControlFields), len(debInfo.ControlFields))
	}
	if ver.UploadedBy != "reconcile" {
		t.Errorf("UploadedBy: got %s, want %s", ver.UploadedBy, "reconcile")
	}
}
