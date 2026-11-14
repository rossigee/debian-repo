package templates

import (
	"strings"
	"testing"
)

// A package's Description comes from attacker-controlled .deb control data.
// It must reach the rendered page escaped, never as live markup.
func TestRenderPackageDetailEscapesDescription(t *testing.T) {
	payload := `<script>alert(1)</script><img src=x onerror=alert(2)>`

	out, err := NewLoader().RenderPackageDetail(&PackageDetailPageData{
		PackageName: "evil",
		Description: payload,
	})
	if err != nil {
		t.Fatalf("RenderPackageDetail: %v", err)
	}

	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Errorf("rendered page contains unescaped script payload")
	}
	if strings.Contains(out, "<img src=x onerror=alert(2)>") {
		t.Errorf("rendered page contains unescaped img payload")
	}
	if !strings.Contains(out, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("expected escaped script payload in rendered page")
	}
}
