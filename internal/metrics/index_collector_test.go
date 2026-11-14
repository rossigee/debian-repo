package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

type fakeIndex struct {
	packagesBySuite map[string]int
}

func (f *fakeIndex) ListDistributions() []string {
	suites := make([]string, 0, len(f.packagesBySuite))
	for s := range f.packagesBySuite {
		suites = append(suites, s)
	}
	return suites
}

func (f *fakeIndex) CountPackages(suite string) int { return f.packagesBySuite[suite] }

func (f *fakeIndex) ListArchitectures(string) []string { return nil }

func (f *fakeIndex) SnapshotGen() uint64 { return 0 }

// gather registers the collector on a private registry and returns the samples
// for one metric family, keyed by the value of its repo label ("" when unlabelled).
func gather(t *testing.T, c prometheus.Collector, name string) map[string]float64 {
	t.Helper()
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	out := make(map[string]float64)
	for _, fam := range families {
		if fam.GetName() != name {
			continue
		}
		for _, m := range fam.GetMetric() {
			out[repoLabel(m)] = m.GetGauge().GetValue()
		}
	}
	return out
}

func repoLabel(m *dto.Metric) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == "repo" {
			return l.GetValue()
		}
	}
	return ""
}

func twoRepos() []RepoIndex {
	return []RepoIndex{
		{ID: "stable-repo", Index: &fakeIndex{packagesBySuite: map[string]int{"stable": 3, "testing": 2}}},
		{ID: "other-repo", Index: &fakeIndex{packagesBySuite: map[string]int{"stable": 5}}},
	}
}

// By default the totals cover every repository, with no repo label, so the
// series set is the same as before.
func TestIndexCollectorSumsAcrossRepositoriesByDefault(t *testing.T) {
	c := newIndexCollector(twoRepos(), false)

	dists := gather(t, c, "debian_repo_distributions_count")
	if len(dists) != 1 || dists[""] != 3 {
		t.Errorf("distributions = %v, want one unlabelled series with 3", dists)
	}
	pkgs := gather(t, c, "debian_repo_packages_total")
	if len(pkgs) != 1 || pkgs[""] != 10 {
		t.Errorf("packages = %v, want one unlabelled series with 10", pkgs)
	}
}

// With per-repository reporting each repository gets its own labelled series.
func TestIndexCollectorPerRepositoryWhenEnabled(t *testing.T) {
	c := newIndexCollector(twoRepos(), true)

	dists := gather(t, c, "debian_repo_distributions_count")
	if dists["stable-repo"] != 2 || dists["other-repo"] != 1 {
		t.Errorf("per-repo distributions = %v, want stable-repo=2 other-repo=1", dists)
	}
	pkgs := gather(t, c, "debian_repo_packages_total")
	if pkgs["stable-repo"] != 5 || pkgs["other-repo"] != 5 {
		t.Errorf("per-repo packages = %v, want stable-repo=5 other-repo=5", pkgs)
	}
}
