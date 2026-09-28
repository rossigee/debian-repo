# Template System

Modern, modular template system for debian-repo HTML pages.

## Architecture

Templates are organized into logical directories:

### Directory Structure

```
internal/templates/
├── components/         # Reusable HTML components
│   ├── navbar.html     # Navigation bar with burger menu
│   └── theme-toggle.html # Light/dark theme toggle button
├── pages/              # Full page templates
│   ├── index-cards.html # Repository index (card grid)
│   ├── install.html    # Installation instructions
│   └── gpgkey.html     # GPG key information
├── static/             # CSS and JavaScript files
│   ├── common.css      # Theme variables and global styles
│   ├── navbar.css      # Navigation styling
│   ├── index.css       # Index page styles
│   ├── install.css     # Install page styles
│   ├── gpgkey.css      # GPG key page styles
│   ├── theme.js        # Theme toggle logic
│   └── navbar.js       # Mobile burger menu logic
├── loader.go           # Template loading and rendering
└── types.go            # Data structures for template context
```

## Design System

### CSS Organization

**Theme Variables** (common.css):
- Color palette: light/dark mode support
- Spacing scale
- Typography scale
- Shadows and transitions

**Component Styles**:
- `navbar.css` - Navigation bar, burger menu, theme toggle
- `index.css` - Package listing, distribution sections
- `install.css` - Key info boxes, instruction blocks
- `gpgkey.css` - Key details, UID display

### Light/Dark Mode

Theme is managed through:
1. CSS variables with system preference detection
2. localStorage for persistent user preference
3. `data-theme` attribute on `<html>` element
  - `light` - Forces light mode
  - `dark` - Forces dark mode
  - No attribute - Uses system preference

## Template Usage

### Components

Components are reusable template fragments included in pages:

```html
{{template "components/navbar" .}}
{{template "components/theme-toggle" .}}
```

Components expect a context with:
- `CurrentPage` - Current page identifier (index, install, gpg)
- `NavItems` - Map of page names to URLs

### Pages

Each page is a complete HTML document. Render via the Loader:

```go
loader := templates.NewLoader()

// Render the index page
html, err := loader.RenderIndexCards(&CardIndexPageData{
	TotalPackages: 42,
	Suites:        [...],
})

// Render install page
html, err := loader.RenderInstall(&InstallPageData{
  RepositoryURL: "https://debs.example.com",
  KeyID: "0x1234...",
})

// Render GPG key page
html, err := loader.RenderGPGKey(&GPGKeyPageData{
  KeyFingerprint: "...",
  PublicKeyBlock: "...",
})
```

## Development

### Adding a New Page

1. **Create template** in `pages/pagename.html`:
   ```html
   {{define "pages/pagename.html"}}<!DOCTYPE html>
   <html>
   <head>
     <meta charset="UTF-8">
     <title>{{ .Title }}</title>
     <link rel="stylesheet" href="/static/common.css">
     <link rel="stylesheet" href="/static/navbar.css">
     <link rel="stylesheet" href="/static/pagename.css">
   </head>
   <body>
   {{template "components/navbar" .}}
   <header>
   {{template "components/theme-toggle" .}}
   <h1>{{ .PageHeader }}</h1>
   </header>
   <main>
   {{ .Content }}
   </main>
   <footer>...</footer>
   </body>
   </html>
   {{end}}
   ```

2. **Create data structure** in `types.go`:
   ```go
   type PageNamePageData struct {
     PageData
     Field1 string
     Field2 []Item
   }
   ```

3. **Add render method** to `loader.go`:
   ```go
   func (l *Loader) RenderPageName(data *PageNamePageData) (string, error) {
     if data.NavItems == nil {
       data.NavItems = defaultNavItems()
     }
     data.CurrentPage = "pagename"
     // ... rendering logic
   }
   ```

4. **Create styles** in `static/pagename.css` if needed

5. **Register the pattern** in the embed directive if not already covered

### Adding a New Component

1. **Create template** in `components/name.html`
2. **Document expected context** in component's HTML comments
3. Reference in pages via `{{template "components/name" .}}`

### Styling Guide

- Use CSS variables for colors, spacing, shadows
- Ensure responsive design (768px breakpoint for mobile)
- Support both light and dark themes via `:root[data-theme]`
- Use semantic class names (`.pkg-name`, `.nav-item`, etc.)
- Keep styles scoped to their components

## Embedding

Templates are embedded at build time using Go's `embed` package. No external files required at runtime.

The `//go:embed` directive in `loader.go` includes:
- `layout/*.html`
- `components/*.html`
- `pages/*.html`

CSS and JS files are served via HTTP routes and are not embedded (served from filesystem).

## Performance Considerations

- Templates are parsed once and cached in memory
- Component includes are resolved at parse time
- No runtime template recompilation
- Suitable for high-concurrency serving
