package model

// ChecksumSidecarV1 caches pre-computed checksums and control metadata for a .deb file
// Written at upload time, read during reconciliation to skip expensive re-hashing
// Only used as a performance cache — reconciliation validates via ETag before trusting
type ChecksumSidecarV1 struct {
	FormatVersion int               `json:"format_version"` // 1
	Package       string            `json:"package"`
	Version       string            `json:"version"`
	Architecture  string            `json:"architecture"`
	ControlFields map[string]string `json:"control_fields"`
	MD5           string            `json:"md5"`
	SHA1          string            `json:"sha1"`
	SHA256        string            `json:"sha256"`
	Size          int64             `json:"size"`
	ETag          string            `json:"etag"`       // MinIO ETag at write time — used for staleness detection only, NOT as a checksum substitute
	WrittenAt     string            `json:"written_at"` // RFC3339 timestamp
}
