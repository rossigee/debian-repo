package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/templates"
)

func seedSuiteServer(t *testing.T) *Server {
	t.Helper()
	s := newTestServer(t) // seeds test-pkg 1.0.0/amd64 in stable/main
	s.templateLoader = templates.NewLoader()

	onlyLibvirtd := &model.PackageVersion{
		Version: "11.0.0", Architecture: "amd64",
		Filename:      "pool/main/libvirtd-tools_11.0.0_amd64.deb",
		Checksums:     model.Checksums{SHA256: "aaa", Size: 42},
		ControlFields: map[string]string{"Package": "libvirtd-tools"},
		UploadedAt:    time.Now(),
	}
	if err := s.registry.All()[0].IndexMgr.AddPackageVersion("libvirtd", "main", "libvirtd-tools", onlyLibvirtd); err != nil {
		t.Fatalf("AddPackageVersion failed: %v", err)
	}

	shared := &model.PackageVersion{
		Version: "2.0.0", Architecture: "amd64",
		Filename:      "pool/main/tree_2.0.0_amd64.deb",
		Checksums:     model.Checksums{SHA256: "bbb", Size: 10},
		ControlFields: map[string]string{"Package": "tree"},
		UploadedAt:    time.Now(),
	}
	for _, suite := range []string{"stable", "libvirtd"} {
		if err := s.registry.All()[0].IndexMgr.AddPackageVersion(suite, "main", "tree", shared); err != nil {
			t.Fatalf("AddPackageVersion failed: %v", err)
		}
	}
	return s
}

func getCards(t *testing.T, s *Server, target string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", target, nil)
	rec := httptest.NewRecorder()
	s.handleIndexCards(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: expected 200, got %d", target, rec.Code)
	}
	return rec.Code, rec.Body.String()
}

func TestIndexCardsShowsSuitePills(t *testing.T) {
	s := seedSuiteServer(t)
	_, body := getCards(t, s, "/")

	// Suite tabs with counts: All (3) + libvirtd (2) + stable (2).
	for _, want := range []string{`href="/"`, `href="/?suite=libvirtd"`, `href="/?suite=stable"`, "All (3)", "libvirtd (2)", "stable (2)"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected homepage to contain %q", want)
		}
	}

	// Cards carry suite pills; the shared package shows both suites.
	for _, want := range []string{`class="suite-pill">libvirtd<`, `class="suite-pill">stable<`} {
		if !strings.Contains(body, want) {
			t.Errorf("expected homepage to contain %q", want)
		}
	}
}

func TestIndexCardsSuiteFilter(t *testing.T) {
	s := seedSuiteServer(t)

	_, body := getCards(t, s, "/?suite=libvirtd")
	if !strings.Contains(body, "libvirtd-tools") || !strings.Contains(body, "tree") {
		t.Fatal("libvirtd filter must show libvirtd-tools and tree")
	}
	if strings.Contains(body, "test-pkg") {
		t.Fatal("libvirtd filter must not show stable-only test-pkg")
	}
	if !strings.Contains(body, "2 packages") || !strings.Contains(body, "in suite") {
		t.Fatal("filtered page must show filtered count and suite name")
	}

	_, body = getCards(t, s, "/?suite=stable")
	if !strings.Contains(body, "test-pkg") || !strings.Contains(body, "tree") {
		t.Fatal("stable filter must show test-pkg and tree")
	}
	if strings.Contains(body, "libvirtd-tools") {
		t.Fatal("stable filter must not show libvirtd-only libvirtd-tools")
	}
}

func TestIndexCardsUnknownSuite(t *testing.T) {
	s := seedSuiteServer(t)

	_, body := getCards(t, s, "/?suite=nosuch")
	if strings.Contains(body, "test-pkg") || strings.Contains(body, "tree") {
		t.Fatal("unknown suite must show no packages")
	}
	// Tabs remain so the user can navigate back.
	if !strings.Contains(body, `href="/"`) {
		t.Fatal("unknown suite page must still render suite tabs")
	}
}

func TestIndexCardsBadSuite(t *testing.T) {
	s := seedSuiteServer(t)

	req := httptest.NewRequest("GET", "/?suite=../x", nil)
	rec := httptest.NewRecorder()
	s.handleIndexCards(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
