// Package feed provides syndication feed generation (Atom, RSS).
package feed

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
)

// atomFeed represents the root Atom feed element
type atomFeed struct {
	XMLName xml.Name    `xml:"http://www.w3.org/2005/Atom feed"`
	ID      string      `xml:"id"`
	Title   string      `xml:"title"`
	Updated string      `xml:"updated"`
	Entries []atomEntry `xml:"entry"`
	Links   []atomLink  `xml:"link"`
}

// atomEntry represents a single feed entry
type atomEntry struct {
	XMLName   xml.Name   `xml:"entry"`
	ID        string     `xml:"id"`
	Title     string     `xml:"title"`
	Updated   string     `xml:"updated"`
	Summary   string     `xml:"summary,omitempty"`
	Links     []atomLink `xml:"link"`
	Published string     `xml:"published"`
	Author    atomAuthor `xml:"author"`
}

// atomAuthor represents feed entry author
type atomAuthor struct {
	XMLName xml.Name `xml:"author"`
	Name    string   `xml:"name"`
}

// atomLink represents a feed link
type atomLink struct {
	XMLName xml.Name `xml:"link"`
	Rel     string   `xml:"rel,attr,omitempty"`
	Href    string   `xml:"href,attr"`
	Type    string   `xml:"type,attr,omitempty"`
}

// RenderAtomFeed generates an Atom feed from package entries.
// repoURL is the base URL of the repository (e.g. "https://debs.example.com").
// suite is the distribution suite (e.g. "stable").
// entries are the package versions to include in the feed.
func RenderAtomFeed(repoURL, suite string, entries []index.FeedEntry) ([]byte, error) {
	now := time.Now().UTC()
	feedID := fmt.Sprintf("urn:debian-repo:%s:%s", repoURL, suite)
	feedTitle := fmt.Sprintf("Debian Repository %s Suite", suite)

	atomEntries := make([]atomEntry, 0, len(entries))
	for _, e := range entries {
		entryID := fmt.Sprintf("urn:debian-repo:%s:%s:%s:%s:%s", repoURL, suite, e.Package, e.Version, e.Architecture)
		entryTitle := fmt.Sprintf("%s %s (%s)", e.Package, e.Version, e.Architecture)

		// Extract description from control fields
		description := e.ControlFields["Description"]
		if description == "" {
			description = "(No description available)"
		}

		// Sanitize HTML content
		summary := html.EscapeString(description)

		// Pool file link
		poolLink := fmt.Sprintf("%s/%s", repoURL, e.Filename)

		updated := e.UploadedAt.UTC().Format(time.RFC3339)

		atomEntries = append(atomEntries, atomEntry{
			ID:        entryID,
			Title:     entryTitle,
			Updated:   updated,
			Published: updated,
			Summary:   summary,
			Author:    atomAuthor{Name: "Debian Repository"},
			Links: []atomLink{
				{
					Rel:  "alternate",
					Href: poolLink,
					Type: "application/x-deb",
				},
			},
		})
	}

	feed := atomFeed{
		ID:      feedID,
		Title:   feedTitle,
		Updated: now.Format(time.RFC3339),
		Entries: atomEntries,
		Links: []atomLink{
			{
				Rel:  "self",
				Href: fmt.Sprintf("%s/dists/%s/feed.atom", repoURL, suite),
				Type: "application/atom+xml",
			},
			{
				Rel:  "alternate",
				Href: repoURL,
				Type: "text/html",
			},
		},
	}

	// Marshal to XML with declaration
	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(feed); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
