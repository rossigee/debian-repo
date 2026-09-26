// Package templates handles HTML template loading, rendering, and static asset serving.
package templates

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"sync"
)

//go:embed layout/*.html components/*.html pages/*.html static/*
var templateFS embed.FS

var (
	templateCache map[string]*template.Template
	cacheMu       sync.RWMutex
)

func init() {
	templateCache = make(map[string]*template.Template)
}

// Loader provides template rendering capabilities
type Loader struct {
	funcMap template.FuncMap
}

// NewLoader creates a new template loader
func NewLoader() *Loader {
	return &Loader{
		funcMap: template.FuncMap{
			"safeHTML": func(s string) template.HTML {
				return template.HTML(s)
			},
			"safeAttr": func(s string) template.HTMLAttr {
				return template.HTMLAttr(s)
			},
			"formatBytes": func(size int64) string {
				const unit = 1024
				if size < unit {
					return fmt.Sprintf("%d B", size)
				}
				div, exp := int64(unit), 0
				for n := size / unit; n >= unit; n /= unit {
					div *= unit
					exp++
				}
				units := []string{"B", "KB", "MB", "GB", "TB"}
				return fmt.Sprintf("%.1f %s", float64(size)/float64(div), units[exp])
			},
			"truncateHash": func(hash string) string {
				if len(hash) > 16 {
					return hash[:16]
				}
				return hash
			},
		},
	}
}

// parseTemplates parses templates with components
func (l *Loader) parseTemplates(pagePattern string) (*template.Template, error) {
	// Check cache first
	cacheMu.RLock()
	if tmpl, exists := templateCache[pagePattern]; exists {
		cacheMu.RUnlock()
		return tmpl, nil
	}
	cacheMu.RUnlock()

	// Parse all templates in order: components, then pages
	tmpl, err := template.New("").Funcs(l.funcMap).ParseFS(templateFS, "components/*.html")
	if err != nil {
		return nil, fmt.Errorf("failed to parse components: %w", err)
	}

	tmpl, err = tmpl.ParseFS(templateFS, pagePattern)
	if err != nil {
		return nil, fmt.Errorf("failed to parse page %s: %w", pagePattern, err)
	}

	// Cache it
	cacheMu.Lock()
	templateCache[pagePattern] = tmpl
	cacheMu.Unlock()

	return tmpl, nil
}

// RenderIndex renders the index page
func (l *Loader) RenderIndex(data *IndexPageData) (string, error) {
	if data.NavItems == nil {
		data.NavItems = map[string]string{
			"index":   "/",
			"install": "/install.html",
			"gpg":     "/gpg-key.html",
		}
	}

	tmpl, err := l.parseTemplates("pages/index.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "pages/index.html", data); err != nil {
		return "", fmt.Errorf("failed to render index: %w", err)
	}

	return buf.String(), nil
}

// RenderInstall renders the install page
func (l *Loader) RenderInstall(data *InstallPageData) (string, error) {
	if data.NavItems == nil {
		data.NavItems = map[string]string{
			"index":   "/",
			"install": "/install.html",
			"gpg":     "/gpg-key.html",
		}
	}
	data.CurrentPage = "install"
	if data.Suite == "" {
		data.Suite = "stable"
	}

	tmpl, err := l.parseTemplates("pages/install.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "pages/install.html", data); err != nil {
		return "", fmt.Errorf("failed to render install: %w", err)
	}

	return buf.String(), nil
}

// RenderGPGKey renders the GPG key page
func (l *Loader) RenderGPGKey(data *GPGKeyPageData) (string, error) {
	if data.NavItems == nil {
		data.NavItems = map[string]string{
			"index":   "/",
			"install": "/install.html",
			"gpg":     "/gpg-key.html",
		}
	}
	data.CurrentPage = "gpg"

	tmpl, err := l.parseTemplates("pages/gpgkey.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "pages/gpgkey.html", data); err != nil {
		return "", fmt.Errorf("failed to render GPG key page: %w", err)
	}

	return buf.String(), nil
}

// RenderCardIndex renders the card-based index page
func (l *Loader) RenderCardIndex(data *CardIndexPageData) (string, error) {
	if data.NavItems == nil {
		data.NavItems = map[string]string{
			"index":   "/",
			"install": "/install.html",
			"gpg":     "/gpg-key.html",
		}
	}
	data.CurrentPage = "index"

	tmpl, err := l.parseTemplates("pages/index-cards.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "pages/index-cards.html", data); err != nil {
		return "", fmt.Errorf("failed to render card index: %w", err)
	}

	return buf.String(), nil
}

// RenderPackageDetail renders a package detail page
func (l *Loader) RenderPackageDetail(data *PackageDetailPageData) (string, error) {
	if data.NavItems == nil {
		data.NavItems = map[string]string{
			"index":   "/",
			"install": "/install.html",
			"gpg":     "/gpg-key.html",
		}
	}
	data.CurrentPage = "packages"

	tmpl, err := l.parseTemplates("pages/package-detail.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "pages/package-detail.html", data); err != nil {
		return "", fmt.Errorf("failed to render package detail: %w", err)
	}

	return buf.String(), nil
}

// RenderProfile renders the user profile page
func (l *Loader) RenderProfile(data *UserProfilePageData) (string, error) {
	if data.NavItems == nil {
		data.NavItems = map[string]string{
			"index":   "/",
			"install": "/install.html",
			"gpg":     "/gpg-key.html",
		}
	}
	data.CurrentPage = "profile"
	data.Title = "User Profile"
	data.PageHeader = "👤 Profile"

	tmpl, err := l.parseTemplates("pages/profile.html")
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "pages/profile.html", data); err != nil {
		return "", fmt.Errorf("failed to render profile: %w", err)
	}

	return buf.String(), nil
}

// GetStaticFile returns the content of a static file (CSS/JS)
func GetStaticFile(filename string) ([]byte, error) {
	// Security: prevent directory traversal
	if bytes.Contains([]byte(filename), []byte("..")) || bytes.HasPrefix([]byte(filename), []byte("/")) {
		return nil, fmt.Errorf("invalid file path")
	}

	// Read from embedded filesystem
	data, err := templateFS.ReadFile("static/" + filename)
	if err != nil {
		return nil, fmt.Errorf("file not found: %w", err)
	}

	return data, nil
}
