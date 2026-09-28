package templates

import (
	"html/template"
	"time"
)

// PageData is the base context for all page renders
type PageData struct {
	Title           string
	CurrentPage     string
	PageHeader      string
	PageSubtitle    template.HTML
	PageStyles      string
	NavItems        map[string]string
	VersionString   string
	LastUpdated     time.Time
	UserEmail       string // User email if authenticated
	UserName        string // User name if authenticated
	IsAuthenticated bool   // True if user has valid OIDC token
	OIDCEnabled     bool   // True if OIDC is configured
}

// InstallPageData contains data for the installation instructions page
type InstallPageData struct {
	PageData
	RepositoryURL  string
	KeyFingerprint string
	KeyID          string
	Suite          string
}

// GPGKeyPageData contains data for the GPG key page
type GPGKeyPageData struct {
	PageData
	KeyFingerprint string
	KeyID          string
	KeyUIDs        []string
	KeyCreated     time.Time
	KeyExpires     time.Time
	PublicKeyBlock string
}

// UserProfilePageData contains data for the user profile page
type UserProfilePageData struct {
	PageData
	Email        string
	Name         string
	Organization string
}

// CardIndexPageData contains data for the card-based index page
type CardIndexPageData struct {
	PageData
	TotalPackages int
	Packages      []PackageCard
	Suites        []SuiteTab
	ActiveSuite   string // empty = all suites
	LastUpdated   time.Time
}

// SuiteTab is one suite filter tab on the card index page
type SuiteTab struct {
	Name   string
	Count  int
	Active bool
}

// PackageCard represents a package in card view
type PackageCard struct {
	Name          string
	LatestVersion string
	Description   string
	Section       string
	Maintainer    string
	VersionCount  int
	ArchCount     int
	Architectures []string // List of architecture names (e.g., "amd64", "arm64")
	Suites        []string // Suites containing this package (e.g., "stable", "libvirtd")
}

// PackageDetailPageData contains data for a package detail page
type PackageDetailPageData struct {
	PageData
	PackageName string
	Description string
	Section     string
	Priority    string
	Maintainer  string
	Depends     string
	Versions    []PackageVersionDetail
}

// PackageVersionDetail represents a package version with distribution info
type PackageVersionDetail struct {
	Version       string
	Architecture  string
	Size          int64
	SHA256        string
	MD5           string
	SHA1          string
	Distributions []string
}
