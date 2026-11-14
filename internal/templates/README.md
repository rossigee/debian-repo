# Template System

HTML pages for debian-repo, rendered server-side from embedded templates and
static assets.

## Layout

```
internal/templates/
├── components/              # Named template fragments included in pages
│   ├── navbar.html          # {{define "navbar"}}: navigation bar and burger menu
│   ├── theme-toggle.html    # {{define "theme-toggle"}}: light/dark toggle button
│   └── user-menu.html       # {{define "user-menu"}}: login/profile menu
├── pages/                   # One complete HTML document per page
│   ├── index-cards.html     # Repository index (card grid, suite tabs)
│   ├── install.html         # Installation instructions
│   ├── gpgkey.html          # GPG key information
│   ├── package-detail.html  # Package detail page
│   └── profile.html         # User profile (requires login)
├── static/                  # CSS, JavaScript and other static files
│   ├── common.css           # Theme variables (colours, shadows) and global styles
│   ├── navbar.css           # Navigation bar, burger menu
│   ├── cards.css            # Card index page
│   ├── install.css          # Installation page
│   ├── gpgkey.css           # GPG key page
│   ├── package-detail.css   # Package detail page
│   ├── profile.css          # Profile page
│   ├── user-menu.css        # User menu
│   ├── theme.js             # Theme toggle
│   ├── navbar.js            # Burger menu
│   ├── user-menu.js         # User menu behaviour
│   ├── favicon.svg
│   └── robots.txt
├── loader.go                # Parsing, caching and rendering of pages
├── types.go                 # Data structures passed to the templates
└── README.md
```

## Embedding

The `//go:embed components/*.html pages/*.html static/*` directive in
`loader.go` embeds every page, component and static file into the binary. No
template or asset files are needed at runtime.

Static files are served from the embedded filesystem by the `/static/` route
(`handleStatic` in `internal/apiserver`).

Templates are parsed once and cached in memory.

## Design System

### CSS

`common.css` defines the theme variables: text colours, and shadows
(`--shadow-sm`, `--shadow-md`). Each page's stylesheet is listed in the layout
above. Pages share `common.css` and `navbar.css`.

### Light and dark mode

The theme is chosen in this order:

1. The `data-theme` attribute on `<html>`: `light` or `dark` forces that theme.
2. Otherwise the system preference applies (CSS `prefers-color-scheme`).

`theme.js` sets `data-theme` from the user's toggle choice. The choice is kept
in `localStorage`.

## Rendering

Components and pages are rendered through the `Loader`:

```go
loader := templates.NewLoader()

html, err := loader.RenderCardIndex(&templates.CardIndexPageData{ /* ... */ })
html, err := loader.RenderInstall(&templates.InstallPageData{
    RepositoryURL:  "https://debs.example.com/stable",
    KeyID:          "0x1234...",
    KeyFingerprint: "...",
    Suite:          "stable",
})
html, err := loader.RenderGPGKey(&templates.GPGKeyPageData{ /* ... */ })
html, err := loader.RenderPackageDetail(&templates.PackageDetailPageData{ /* ... */ })
html, err := loader.RenderProfile(&templates.UserProfilePageData{ /* ... */ })
```

Every page embeds `PageData`, which holds the title, the current page, the
navbar links (`NavItems`) and the authentication state. If a handler does not
set `NavItems`, the render method fills in the default links.

Components are included with `{{template "navbar" .}}`, `{{template "theme-toggle" .}}`
or `{{template "user-menu" .}}`. A component reads the same page data as the
page that includes it, so `CurrentPage` and `NavItems` must be set.

## Adding a page

1. Create `pages/<name>.html` with `{{define "pages/<name>.html"}}` around the
   document, as the existing pages do. It is embedded automatically.
2. Add a data struct to `types.go` that embeds `PageData`.
3. Add a `Render<Name>` method to `loader.go` that sets the defaults and
   executes the template.
4. Add `static/<name>.css` if the page needs styles. It is embedded and served
   automatically.

## Adding a component

1. Create `components/<name>.html` with `{{define "<name>"}}`.
2. Document the expected context in an HTML comment at the top of the file.
3. Include it in pages with `{{template "<name>" .}}`.

## Styling guide

- Use the theme variables in `common.css` for colours and shadows.
- Support both themes through `:root[data-theme]` as well as the system preference.
- Keep the mobile layout working: the navbar switches to a burger menu below 768px.
- Use semantic class names, and keep each stylesheet to the component it styles.
