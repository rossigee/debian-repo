package feed

import (
	"encoding/xml"
	"testing"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
)

func TestRenderAtomFeed_ValidXML(t *testing.T) {
	repoURL := "https://debs.example.com"
	suite := "stable"

	now := time.Now()
	entries := []index.FeedEntry{
		{
			Package:       "test-pkg",
			Version:       "1.0.0",
			Architecture:  "amd64",
			Component:     "main",
			Filename:      "pool/main/t/test-pkg/test-pkg_1.0.0_amd64.deb",
			UploadedAt:    now,
			ControlFields: map[string]string{"Description": "A test package"},
		},
		{
			Package:       "another-pkg",
			Version:       "2.0.0",
			Architecture:  "arm64",
			Component:     "main",
			Filename:      "pool/main/a/another-pkg/another-pkg_2.0.0_arm64.deb",
			UploadedAt:    now.Add(-1 * time.Minute),
			ControlFields: map[string]string{"Description": "Another test package"},
		},
	}

	data, err := RenderAtomFeed(repoURL, suite, entries)
	if err != nil {
		t.Fatalf("RenderAtomFeed failed: %v", err)
	}

	if len(data) == 0 {
		t.Error("RenderAtomFeed returned empty data")
	}

	// Unmarshal to verify it's valid XML
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		t.Fatalf("Generated feed is not valid XML: %v", err)
	}

	// Verify feed structure
	if feed.Title != "Debian Repository stable Suite" {
		t.Errorf("Expected title 'Debian Repository stable Suite', got %q", feed.Title)
	}

	if len(feed.Entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(feed.Entries))
	}

	// Verify first entry
	if feed.Entries[0].Title != "test-pkg 1.0.0 (amd64)" {
		t.Errorf("Expected title 'test-pkg 1.0.0 (amd64)', got %q", feed.Entries[0].Title)
	}

	if len(feed.Entries[0].Links) != 1 {
		t.Errorf("Expected 1 link in entry, got %d", len(feed.Entries[0].Links))
	}

	// Verify feed has self link
	var hasSelfLink bool
	for _, link := range feed.Links {
		if link.Rel == "self" {
			hasSelfLink = true
			break
		}
	}
	if !hasSelfLink {
		t.Error("Feed should have a self link")
	}

	t.Logf("✓ Atom feed generated and validated successfully")
}

func TestRenderAtomFeed_EmptyEntries(t *testing.T) {
	data, err := RenderAtomFeed("https://example.com", "testing", []index.FeedEntry{})
	if err != nil {
		t.Fatalf("RenderAtomFeed failed on empty entries: %v", err)
	}

	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		t.Fatalf("Generated feed is not valid XML: %v", err)
	}

	if len(feed.Entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(feed.Entries))
	}

	t.Logf("✓ Empty feed generated successfully")
}
