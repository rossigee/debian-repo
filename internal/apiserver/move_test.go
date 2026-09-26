package apiserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/templates"
)

func seedMoveServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t)

	libvirtd := &model.PackageVersion{
		Version:      "11.0.0",
		Architecture: "amd64",
		Filename:     "pool/main/libvirtd-tools_11.0.0_amd64.deb",
		Checksums:    model.Checksums{MD5: "m", SHA1: "s", SHA256: "abc", Size: 42},
		ControlFields: map[string]string{
			"Package": "libvirtd-tools", "Version": "11.0.0", "Architecture": "amd64",
		},
		UploadedAt: time.Now(),
		UploadedBy: "ci-upload",
	}
	if err := s.registry.All()[0].IndexMgr.AddPackageVersion("stable", "main", "libvirtd-tools", libvirtd); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	tree := &model.PackageVersion{
		Version:      "2.0.0",
		Architecture: "amd64",
		Filename:     "pool/main/tree_2.0.0_amd64.deb",
		Checksums:    model.Checksums{SHA256: "def", Size: 10},
		ControlFields: map[string]string{
			"Package": "tree", "Version": "2.0.0", "Architecture": "amd64",
		},
		UploadedAt: time.Now(),
		UploadedBy: "ci-upload",
	}
	if err := s.registry.All()[0].IndexMgr.AddPackageVersion("stable", "main", "tree", tree); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}
	return s
}

func doMove(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/admin/move", strings.NewReader(body))
	req.Header.Set("X-CI-Identity", "test")
	rec := httptest.NewRecorder()
	s.handleMovePackages(rec, req)
	return rec
}

type moveResponse struct {
	Status    string         `json:"status"`
	FromSuite string         `json:"from_suite"`
	ToSuite   string         `json:"to_suite"`
	Moved     []movedPackage `json:"moved"`
	Count     int            `json:"count"`
}

func decodeMove(t *testing.T, rec *httptest.ResponseRecorder) moveResponse {
	t.Helper()
	var resp moveResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode move response: %v", err)
	}
	return resp
}

func TestHandleMoveDryRun(t *testing.T) {
	s := seedMoveServer(t)

	rec := doMove(t, s, `{"from_suite":"stable","to_suite":"libvirtd","packages":["libvirtd*"],"dry_run":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMove(t, rec)
	if resp.Status != "dry_run" || resp.Count != 1 {
		t.Fatalf("expected dry_run with 1 match, got %+v", resp)
	}
	if resp.Moved[0].Package != "libvirtd-tools" || resp.Moved[0].Filename != "pool/main/libvirtd-tools_11.0.0_amd64.deb" {
		t.Fatalf("unexpected move candidate: %+v", resp.Moved[0])
	}

	// Dry run must not mutate: libvirtd suite must not exist, stable intact.
	if _, ok := s.registry.All()[0].IndexMgr.GetDistribution("libvirtd"); ok {
		t.Fatal("dry run created the destination suite")
	}
	if s.registry.All()[0].IndexMgr.CountPackages("stable") != 3 { // test-pkg + libvirtd-tools + tree
		t.Fatalf("dry run mutated stable: %d packages", s.registry.All()[0].IndexMgr.CountPackages("stable"))
	}
}

func TestHandleMoveApply(t *testing.T) {
	s := seedMoveServer(t)

	rec := doMove(t, s, `{"from_suite":"stable","to_suite":"libvirtd","packages":["libvirtd*"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	resp := decodeMove(t, rec)
	if resp.Status != "moved" || resp.Count != 1 {
		t.Fatalf("expected moved with 1 package, got %+v", resp)
	}

	// Destination holds the package with identical file reference + checksums.
	dest, ok := s.registry.All()[0].IndexMgr.GetDistribution("libvirtd")
	if !ok {
		t.Fatal("destination suite was not created")
	}
	dest.Mu.RLock()
	pkg, ok := dest.Components["main"].Packages["libvirtd-tools"]
	dest.Mu.RUnlock()
	if !ok {
		t.Fatal("libvirtd-tools not found in libvirtd/main")
	}
	got := pkg.Versions["11.0.0:amd64"]
	if got == nil || got.Filename != "pool/main/libvirtd-tools_11.0.0_amd64.deb" || got.Checksums.SHA256 != "abc" {
		t.Fatalf("moved version lost file reference: %+v", got)
	}
	if got.UploadedBy != "ci-upload" {
		t.Fatalf("moved version lost audit trail: %+v", got)
	}

	// Source no longer lists it, but keeps the others.
	src, _ := s.registry.All()[0].IndexMgr.GetDistribution("stable")
	src.Mu.RLock()
	_, stillThere := src.Components["main"].Packages["libvirtd-tools"]
	_, treeKept := src.Components["main"].Packages["tree"]
	src.Mu.RUnlock()
	if stillThere {
		t.Fatal("libvirtd-tools still present in stable after move")
	}
	if !treeKept {
		t.Fatal("unrelated package tree lost from stable after move")
	}

	// Both suites were re-rendered.
	if s.registry.All()[0].IndexMgr.GetRenderedDist("libvirtd") == nil {
		t.Fatal("libvirtd was not rendered after move")
	}
	if s.registry.All()[0].IndexMgr.GetRenderedDist("stable") == nil {
		t.Fatal("stable was not re-rendered after move")
	}
}

func TestHandleMoveValidation(t *testing.T) {
	s := seedMoveServer(t)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing suites", `{}`, http.StatusBadRequest},
		{"same suite", `{"from_suite":"stable","to_suite":"stable"}`, http.StatusBadRequest},
		{"bad suite", `{"from_suite":"../x","to_suite":"libvirtd"}`, http.StatusBadRequest},
		{"bad component", `{"from_suite":"stable","to_suite":"libvirtd","component":"a/b"}`, http.StatusBadRequest},
		{"bad glob", `{"from_suite":"stable","to_suite":"libvirtd","packages":["["]}`, http.StatusBadRequest},
		{"unknown source", `{"from_suite":"nosuch","to_suite":"libvirtd"}`, http.StatusNotFound},
		{"invalid json", `not-json`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rec := doMove(t, s, tc.body); rec.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandleMoveForbidden(t *testing.T) {
	s := seedMoveServer(t)

	req := httptest.NewRequest("POST", "/api/v1/admin/move",
		strings.NewReader(`{"from_suite":"stable","to_suite":"libvirtd","packages":["libvirtd*"]}`))
	req.Header.Set("X-CI-Identity", "nobody")
	rec := httptest.NewRecorder()
	s.handleMovePackages(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleMoveEmptyMatch(t *testing.T) {
	s := seedMoveServer(t)

	rec := doMove(t, s, `{"from_suite":"stable","to_suite":"libvirtd","packages":["nosuchpkg*"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if resp := decodeMove(t, rec); resp.Status != "moved" || resp.Count != 0 {
		t.Fatalf("expected empty move, got %+v", resp)
	}
}

func TestHandleInstallSuiteParam(t *testing.T) {
	s := seedMoveServer(t)
	s.templateLoader = templates.NewLoader()

	req := httptest.NewRequest("GET", "/install.html?suite=libvirtd", nil)
	rec := httptest.NewRecorder()
	s.handleInstall(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "libvirtd") {
		t.Fatal("install page does not reflect ?suite=libvirtd")
	}
	if strings.Contains(rec.Body.String(), "Suites: stable") {
		t.Fatal("install page still hardcodes stable with ?suite=libvirtd")
	}

	// Default stays stable.
	req = httptest.NewRequest("GET", "/install.html", nil)
	rec = httptest.NewRecorder()
	s.handleInstall(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Suites: stable") {
		t.Fatalf("install page default changed: %d", rec.Code)
	}

	// Path traversal rejected.
	req = httptest.NewRequest("GET", "/install.html?suite=../x", nil)
	rec = httptest.NewRecorder()
	s.handleInstall(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad suite, got %d", rec.Code)
	}
}
