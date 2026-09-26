package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsValidFilename(t *testing.T) {
	valid := []string{"foo_1.0_amd64.deb", "a+b~c.d-e_f", "x"}
	for _, v := range valid {
		if !isValidFilename(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	invalid := []string{"", "../x", "a/b", "/abs", ".hidden", "a\\b", "sp ace", "semi;colon", "quote\"", "back`tick", "dollar$"}
	for _, v := range invalid {
		if isValidFilename(v) {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestIsValidStagingKey(t *testing.T) {
	if !isValidStagingKey("_staging/foo_1.0.deb-123", "_staging/") {
		t.Error("expected presign-generated key to be valid")
	}
	// Empty prefix defaults to _staging/.
	if !isValidStagingKey("_staging/foo-1", "") {
		t.Error("expected default prefix to apply")
	}
	invalid := []string{
		"",
		"pool/main/foo_1.0_amd64.deb",  // pool object, not staging
		"_meta/index-snapshot.json.gz", // snapshot object
		"_meta/apt-users.json.gz",      // credential store
		"_staging/../../etc/passwd",    // traversal
		"_staging/sub/dir",             // nested
		"_staging/",                    // bare prefix
		"other/foo-1",                  // wrong prefix
	}
	for _, v := range invalid {
		if isValidStagingKey(v, "_staging/") {
			t.Errorf("expected %q to be invalid", v)
		}
	}
}

func TestHandlePresignRejectsBadFilename(t *testing.T) {
	s := newTestServer(t)
	s.config.UploadMode = "presigned"

	for _, filename := range []string{"../../pool/evil", "pool/main/x.deb", ""} {
		body := `{"suite":"stable","component":"main","filename":"` + filename + `"}`
		// Empty filename is a separate missing-field path; still must not 500.
		req := httptest.NewRequest("POST", "/api/v1/presign", strings.NewReader(body))
		req.Header.Set("X-CI-Identity", "test")
		rec := httptest.NewRecorder()
		s.handlePresign(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("filename %q: expected 400, got %d: %s", filename, rec.Code, rec.Body.String())
		}
	}
}

func TestHandleRegisterRejectsBadStagingKey(t *testing.T) {
	s := newTestServer(t)
	s.config.UploadMode = "presigned"

	for _, key := range []string{
		"pool/main/foo_1.0_amd64.deb",
		"_meta/index-snapshot.json.gz",
		"_staging/../../x",
		"",
	} {
		body := `{"suite":"stable","component":"main","staging_key":"` + key + `"}`
		req := httptest.NewRequest("POST", "/api/v1/register", strings.NewReader(body))
		req.Header.Set("X-CI-Identity", "test")
		rec := httptest.NewRecorder()
		s.handleRegister(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("staging_key %q: expected 400, got %d: %s", key, rec.Code, rec.Body.String())
		}
	}
}
