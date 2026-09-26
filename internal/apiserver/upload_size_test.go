package apiserver

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// buildTestDeb assembles a minimal valid .deb: ar archive with
// debian-binary, control.tar.gz (./control), and data.tar.gz members.
func buildTestDeb(t *testing.T, pkg, version, arch string) []byte {
	t.Helper()

	control := fmt.Sprintf("Package: %s\nVersion: %s\nArchitecture: %s\nDescription: test\n", pkg, version, arch)

	var controlTar bytes.Buffer
	tw := tar.NewWriter(&controlTar)
	if err := tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0644, Size: int64(len(control))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(control)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var controlGz bytes.Buffer
	gz := gzip.NewWriter(&controlGz)
	if _, err := gz.Write(controlTar.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	var dataTar bytes.Buffer
	tw = tar.NewWriter(&dataTar)
	if err := tw.WriteHeader(&tar.Header{Name: "./usr/share/doc/x", Mode: 0644, Size: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var dataGz bytes.Buffer
	gz = gzip.NewWriter(&dataGz)
	if _, err := gz.Write(dataTar.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	var deb bytes.Buffer
	deb.WriteString("!<arch>\n")
	writeArMember(t, &deb, "debian-binary", []byte("2.0\n"))
	writeArMember(t, &deb, "control.tar.gz", controlGz.Bytes())
	writeArMember(t, &deb, "data.tar.gz", dataGz.Bytes())
	return deb.Bytes()
}

func writeArMember(t *testing.T, buf *bytes.Buffer, name string, data []byte) {
	t.Helper()
	header := fmt.Sprintf("%-16s%-12d%-6d%-6d%-8o%-10d`\n", name, 0, 0, 0, 0644, len(data))
	if len(header) != 60 {
		t.Fatalf("bad ar header length %d", len(header))
	}
	buf.WriteString(header)
	buf.Write(data)
	if len(data)%2 != 0 {
		buf.WriteByte('\n')
	}
}

// The direct-upload path must record the real object size and checksums,
// and the body must survive validation (pre-C3-fix it stored 0 bytes with
// Size 0 because validate.Deb drained the request body).
func TestHandleUploadRecordsRealSize(t *testing.T) {
	s := newTestServer(t)
	s.config.UploadMode = "direct"

	deb := buildTestDeb(t, "size-pkg", "1.2.3", "amd64")
	wantSHA := fmt.Sprintf("%x", sha256.Sum256(deb))

	req := httptest.NewRequest("POST", "/api/v1/upload?suite=stable&component=main", bytes.NewReader(deb))
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleUpload(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Status    string                 `json:"status"`
		Package   string                 `json:"package"`
		Checksums map[string]interface{} `json:"checksums"`
		Filename  string                 `json:"filename"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.Checksums["sha256"] != wantSHA {
		t.Fatalf("sha256 mismatch: got %v want %s", resp.Checksums["sha256"], wantSHA)
	}
	if size, _ := resp.Checksums["size"].(float64); int64(size) != int64(len(deb)) {
		t.Fatalf("size mismatch: got %v want %d", resp.Checksums["size"], len(deb))
	}

	// Index entry carries the same real size.
	mgr := s.registry.All()[0].IndexMgr
	dist, ok := mgr.GetDistribution("stable")
	if !ok {
		t.Fatal("stable missing after upload")
	}
	dist.Mu.RLock()
	got := dist.Components["main"].Packages["size-pkg"].Versions["1.2.3:amd64"]
	dist.Mu.RUnlock()
	if got == nil {
		t.Fatal("uploaded version not indexed")
	}
	if got.Checksums.Size != int64(len(deb)) || got.Checksums.SHA256 != wantSHA {
		t.Fatalf("indexed checksums wrong: %+v", got.Checksums)
	}
}

func TestHandleUploadRejectsGarbage(t *testing.T) {
	s := newTestServer(t)
	s.config.UploadMode = "direct"

	req := httptest.NewRequest("POST", "/api/v1/upload?suite=stable&component=main", bytes.NewReader([]byte("not a deb")))
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleUpload(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
