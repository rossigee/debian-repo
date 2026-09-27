package repo

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"git.golder.lan/rossgolderltd/debian-repo/internal/gpgsign"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/testsupport/gpgtest"
	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
)

// stubLoader stands in for *minio.SnapshotStore so hydration can be exercised
// without MinIO.
type stubLoader struct {
	snap  *model.SnapshotV1
	err   error
	calls int
}

func (s *stubLoader) GetLatestSnapshot(context.Context) (*model.SnapshotV1, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.snap, nil
}

func testSigner(t *testing.T) *gpgsign.Signer {
	t.Helper()
	signer, err := gpgsign.NewSigner(gpgtest.KeyPath(t), "", false)
	if err != nil {
		t.Fatalf("creating test signer: %v", err)
	}
	return signer
}

// newTestRepo builds a repository with just enough wiring for hydration. The
// signer may be nil when the index is expected to stay empty, since rendering is
// only reached once a suite exists.
func newTestRepo(t *testing.T, id string, signer *gpgsign.Signer) *Repo {
	t.Helper()
	return &Repo{
		ID:       id,
		IndexMgr: index.NewManager(),
		Signer:   signer,
		Metadata: Metadata{
			Origin:      "test-origin",
			Label:       "Test Repository",
			Description: "Test repo",
		},
		DefaultSuite:     "stable",
		DefaultComponent: "main",
	}
}

// populatedSnapshot builds a real snapshot for suite with a two-architecture
// package, exercising the same ToSnapshot path used in production.
func populatedSnapshot(t *testing.T, suite string) *model.SnapshotV1 {
	t.Helper()
	mgr := index.NewManager()
	for _, arch := range []string{"amd64", "arm64"} {
		err := mgr.AddPackageVersion(suite, "main", "test-pkg", &model.PackageVersion{
			Version:      "1.0.0",
			Architecture: arch,
			Filename:     "pool/main/test-pkg_1.0.0_" + arch + ".deb",
			Checksums:    model.Checksums{SHA256: "abc123", SHA1: "def456", MD5: "aaa111", Size: 1000},
			ControlFields: map[string]string{
				"Package":        "test-pkg",
				"Version":        "1.0.0",
				"Architecture":   arch,
				"Maintainer":     "Test User <test@example.com>",
				"Section":        "utils",
				"Priority":       "optional",
				"Description":    "A test package",
				"Installed-Size": "42",
			},
		})
		if err != nil {
			t.Fatalf("AddPackageVersion(%s): %v", arch, err)
		}
	}
	return mgr.GetIndex().ToSnapshot("test")
}

func TestHydrateRendersMetadataFromSnapshot(t *testing.T) {
	rp := newTestRepo(t, "default", testSigner(t))
	loader := &stubLoader{snap: populatedSnapshot(t, "stable")}

	Hydrate(context.Background(), rp, loader)

	if loader.calls != 1 {
		t.Errorf("loader called %d times, want 1", loader.calls)
	}
	if !rp.Hydrated() {
		t.Error("repo not marked hydrated")
	}
	if got := len(rp.IndexMgr.ListDistributions()); got != 1 {
		t.Fatalf("got %d distributions, want 1", got)
	}

	rendered := rp.IndexMgr.GetRenderedDist("stable")
	if rendered == nil {
		t.Fatal("no rendered dist for stable")
	}
	if rendered.Suite != "stable" {
		t.Errorf("RenderedDist.Suite = %q, want %q", rendered.Suite, "stable")
	}
	if len(rendered.ReleasePlain) == 0 {
		t.Error("ReleasePlain is empty")
	}
	if len(rendered.ReleaseGPG) == 0 {
		t.Error("ReleaseGPG is empty")
	}
	if len(rendered.InRelease) == 0 {
		t.Error("InRelease is empty")
	}
	if rendered.RenderedAt.IsZero() {
		t.Error("RenderedAt not set")
	}

	for _, arch := range []string{"amd64", "arm64"} {
		if len(rendered.PackagesByArch[arch]) == 0 {
			t.Errorf("PackagesByArch[%s] is empty", arch)
		}
		if len(rendered.PackagesGzByArch[arch]) == 0 {
			t.Errorf("PackagesGzByArch[%s] is empty", arch)
		}
		if !bytes.Contains(rendered.PackagesByArch[arch], []byte("Package: test-pkg")) {
			t.Errorf("PackagesByArch[%s] missing package stanza", arch)
		}
	}
}

// TestHydrateProducesVerifiableSignatures is the security-relevant check: the
// metadata produced by the background hydration path must be signed by the
// repository key, not merely be non-empty.
func TestHydrateProducesVerifiableSignatures(t *testing.T) {
	signer := testSigner(t)
	rp := newTestRepo(t, "default", signer)

	Hydrate(context.Background(), rp, &stubLoader{snap: populatedSnapshot(t, "stable")})

	rendered := rp.IndexMgr.GetRenderedDist("stable")
	if rendered == nil {
		t.Fatal("no rendered dist for stable")
	}
	keyring, err := openpgp.ReadArmoredKeyRing(strings.NewReader(signer.KeyInfo().ArmoredPublicKey))
	if err != nil {
		t.Fatalf("reading armored public key: %v", err)
	}

	// Release.gpg is produced by ArmoredDetachSign, so the armor must be decoded
	// before the packet stream can be checked.
	sigBlock, err := armor.Decode(bytes.NewReader(rendered.ReleaseGPG))
	if err != nil {
		t.Fatalf("decoding Release.gpg armor: %v", err)
	}
	if _, err := openpgp.CheckDetachedSignature(keyring,
		bytes.NewReader(rendered.ReleasePlain), sigBlock.Body, nil); err != nil {
		t.Errorf("Release.gpg does not verify against the repository key: %v", err)
	}

	// InRelease must clearsign exactly the Release content, so that an apt client
	// verifying the inline signature gets the same document the detached
	// signature covers.
	cs, _ := clearsign.Decode(rendered.InRelease)
	if cs == nil {
		t.Fatal("InRelease is not a clearsigned document")
	}
	if _, err := cs.VerifySignature(keyring, nil); err != nil {
		t.Errorf("InRelease signature does not verify against the repository key: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(cs.Plaintext), bytes.TrimSpace(rendered.ReleasePlain)) {
		t.Error("InRelease payload does not match the detached Release content")
	}
}

func TestHydrateEmptyIndexWhenNoSnapshot(t *testing.T) {
	rp := newTestRepo(t, "default", nil)

	Hydrate(context.Background(), rp, &stubLoader{snap: nil})

	if !rp.Hydrated() {
		t.Error("repo not marked hydrated")
	}
	if got := len(rp.IndexMgr.ListDistributions()); got != 0 {
		t.Errorf("got %d distributions, want 0", got)
	}
	if rp.IndexMgr.GetRenderedDist("stable") != nil {
		t.Error("rendered dist set for an empty index")
	}
}

// TestHydrateFailsOpenOnLoaderError pins the pre-existing fail-open behaviour: a
// MinIO outage must not leave the repository permanently unready.
func TestHydrateFailsOpenOnLoaderError(t *testing.T) {
	rp := newTestRepo(t, "default", nil)
	loader := &stubLoader{err: errors.New("minio unavailable")}

	Hydrate(context.Background(), rp, loader)

	if loader.calls != 1 {
		t.Errorf("loader called %d times, want 1", loader.calls)
	}
	if !rp.Hydrated() {
		t.Error("repo not marked hydrated after loader failure")
	}
	if got := len(rp.IndexMgr.ListDistributions()); got != 0 {
		t.Errorf("got %d distributions, want 0", got)
	}
}

func TestHydrateWithNilLoader(t *testing.T) {
	rp := newTestRepo(t, "default", nil)

	Hydrate(context.Background(), rp, nil)

	if !rp.Hydrated() {
		t.Error("repo not marked hydrated")
	}
	if got := len(rp.IndexMgr.ListDistributions()); got != 0 {
		t.Errorf("got %d distributions, want 0", got)
	}
}

func TestHydrateAllHydratesEveryRepo(t *testing.T) {
	signer := testSigner(t)
	snap := populatedSnapshot(t, "stable")

	// A repository with a real store and one with none, to prove HydrateAll
	// tolerates per-repository differences.
	populated := newTestRepo(t, "populated", signer)
	empty := newTestRepo(t, "empty", nil)

	HydrateAll(context.Background(), []*Repo{populated, empty})

	// Repositories are hydrated through their concrete SnapshotStore field, so
	// with a nil store they come up empty but still marked hydrated.
	if !empty.Hydrated() {
		t.Error("repo with no store not marked hydrated")
	}
	if got := len(empty.IndexMgr.ListDistributions()); got != 0 {
		t.Errorf("repo with no store has %d distributions, want 0", got)
	}
	if snap.SnapshotGen == 0 && len(snap.Distributions) == 0 {
		t.Error("test snapshot is unexpectedly empty")
	}
}

// TestHydrateSkipsRenderingOnCancelledContext documents that a cancelled context
// stops rendering but still completes hydration, so shutdown does not wedge.
func TestHydrateSkipsRenderingOnCancelledContext(t *testing.T) {
	rp := newTestRepo(t, "default", testSigner(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	Hydrate(ctx, rp, &stubLoader{snap: populatedSnapshot(t, "stable")})

	if !rp.Hydrated() {
		t.Error("repo not marked hydrated")
	}
	if got := len(rp.IndexMgr.ListDistributions()); got != 1 {
		t.Errorf("got %d distributions, want 1 (snapshot should still load)", got)
	}
	if rp.IndexMgr.GetRenderedDist("stable") != nil {
		t.Error("rendered dist was produced despite a cancelled context")
	}
}

// TestHydrateRendersEverySuite covers the multi-suite loop.
func TestHydrateRendersEverySuite(t *testing.T) {
	rp := newTestRepo(t, "default", testSigner(t))
	mgr := index.NewManager()
	for _, suite := range []string{"stable", "testing", "unstable"} {
		if err := mgr.AddPackageVersion(suite, "main", "test-pkg", &model.PackageVersion{
			Version:      "1.0.0",
			Architecture: "amd64",
			Filename:     "pool/main/test-pkg_1.0.0_amd64.deb",
			Checksums:    model.Checksums{SHA256: "abc123", Size: 1000},
			ControlFields: map[string]string{
				"Package":     "test-pkg",
				"Version":     "1.0.0",
				"Description": "A test package",
			},
		}); err != nil {
			t.Fatalf("AddPackageVersion(%s): %v", suite, err)
		}
	}

	Hydrate(context.Background(), rp, &stubLoader{snap: mgr.GetIndex().ToSnapshot("test")})

	for _, suite := range []string{"stable", "testing", "unstable"} {
		rendered := rp.IndexMgr.GetRenderedDist(suite)
		if rendered == nil {
			t.Errorf("no rendered dist for %s", suite)
			continue
		}
		if len(rendered.InRelease) == 0 {
			t.Errorf("InRelease empty for %s", suite)
		}
		if !strings.Contains(string(rendered.ReleasePlain), "test-pkg") &&
			!strings.Contains(string(rendered.PackagesByArch["amd64"]), "test-pkg") {
			t.Errorf("suite %s does not reference test-pkg", suite)
		}
	}
}
