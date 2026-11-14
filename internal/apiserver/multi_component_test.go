package apiserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/aptmeta"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/repo"
)

// Release advertises a Packages file for every component. Each one must be
// served at the path it names, with that component's packages only.
func TestEveryComponentPackagesIsServed(t *testing.T) {
	packagesByComponent := map[string]string{
		"main":    "main-pkg",
		"contrib": "contrib-pkg",
	}

	mgr := index.NewManager()
	for comp, pkg := range packagesByComponent {
		err := mgr.AddPackageVersion("stable", comp, pkg, &model.PackageVersion{
			Version:      "1.0.0",
			Architecture: "amd64",
			Filename:     "pool/" + comp + "/" + pkg + "_1.0.0_amd64.deb",
			ControlFields: map[string]string{
				"Package":      pkg,
				"Version":      "1.0.0",
				"Architecture": "amd64",
			},
		})
		if err != nil {
			t.Fatalf("AddPackageVersion(%s): %v", comp, err)
		}
	}

	dist, ok := mgr.GetDistribution("stable")
	if !ok {
		t.Fatal("stable distribution missing")
	}
	packages, gz, err := aptmeta.RenderAllPackages(dist)
	if err != nil {
		t.Fatalf("RenderAllPackages: %v", err)
	}
	mgr.SetRenderedDist("stable", &model.RenderedDist{
		Suite:           "stable",
		PackagesByKey:   packages,
		PackagesGzByKey: gz,
	})

	rp := &repo.Repo{ID: "multi", IndexMgr: mgr}
	reg, err := repo.NewRegistry([]*repo.Repo{rp})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	s := &Server{registry: reg}

	for comp, pkg := range packagesByComponent {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/dists/stable/"+comp+"/binary-amd64/Packages", nil)
		s.handleDists(rec, withTestRepo(req, rp))

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", comp, rec.Code)
			continue
		}
		body := rec.Body.String()
		if !strings.Contains(body, "Package: "+pkg) {
			t.Errorf("%s: Packages missing its own package %q", comp, pkg)
		}
		for otherComp, otherPkg := range packagesByComponent {
			if otherComp != comp && strings.Contains(body, otherPkg) {
				t.Errorf("%s: Packages leaks %q from %s", comp, otherPkg, otherComp)
			}
		}
	}
}
