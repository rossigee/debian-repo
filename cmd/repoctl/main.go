// Package main is the operator CLI for debian-repo
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/config"
	"git.golder.lan/rossgolderltd/debian-repo/internal/index"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
	"git.golder.lan/rossgolderltd/debian-repo/internal/reconcile"
	"git.golder.lan/rossgolderltd/debian-repo/internal/storage/minio"
	"git.golder.lan/rossgolderltd/debian-repo/internal/version"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	args := os.Args[2:]

	switch command {
	case "-version", "--version", "version":
		fmt.Println("repoctl", version.String())
	case "import":
		cmdImport(args)
	case "reconcile":
		cmdReconcile(args)
	case "users":
		cmdUsers(args)
	case "help":
		printUsage()
	default:
		fmt.Printf("repoctl: unknown command '%s'\n", command)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`repoctl — Debian repository operator CLI

Usage: repoctl [command] [options]

Commands:
  import [--config FILE] [--suite SUITE] [--dry-run|--apply]
      Import/migrate package index from MinIO pool contents
      --suite:   Default suite for pool files not already indexed
                (defaults to the repo default_suite)
      --dry-run: Show what would be imported without making changes
      --apply:   Persist the snapshot to MinIO

  reconcile [--config FILE] [--suite SUITE] [--dry-run|--apply]
      Reconcile index against pool contents (check for drift)
      --suite:   Default suite for pool files not already indexed
                (defaults to the repo default_suite)

  users <subcommand> [options]
      Manage apt Basic-Auth users
      Subcommands: add, list, remove, passwd

  help
      Show this help message

  version
      Show version information
`)
}

// defaultSuiteForConfig resolves the default suite for pool files that are
// not already indexed in any suite: the --suite flag wins, then the first
// repo's default_suite, then "stable" for backward compatibility.
func defaultSuiteForConfig(cfg *config.Config, suiteFlag string) string {
	if suiteFlag != "" {
		return suiteFlag
	}
	if repos, err := config.ResolveRepos(cfg); err == nil && len(repos) > 0 && repos[0].DefaultSuite != "" {
		return repos[0].DefaultSuite
	}
	return "stable"
}

func cmdImport(args []string) {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	configPath := fs.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
	suiteFlag := fs.String("suite", "", "Default suite for unindexed pool files (defaults to repo default_suite)")
	dryRun := fs.Bool("dry-run", false, "Show preview without applying")
	apply := fs.Bool("apply", false, "Apply the import")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *dryRun && *apply {
		fmt.Println("Error: cannot use both --dry-run and --apply")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create MinIO client
	minioClient, err := minio.NewClient(minio.ClientConfig{
		Endpoint:   cfg.Storage.MinIO.Endpoint,
		AccessKey:  cfg.Storage.MinIO.AccessKey,
		SecretKey:  cfg.Storage.MinIO.SecretKey,
		Bucket:     cfg.Storage.MinIO.Bucket,
		UseTLS:     cfg.Storage.MinIO.UseTLS,
		CACertPath: cfg.Storage.MinIO.CACert,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MinIO client: %v\n", err)
		os.Exit(1)
	}

	// Create index manager
	indexMgr := index.NewManager()

	// Create reconciler
	rec := reconcile.NewReconciler(minioClient, indexMgr, defaultSuiteForConfig(cfg, *suiteFlag))

	// Reconcile (scan pool and build index)
	ctx := context.Background()
	fmt.Println("Reconciling index from pool contents...")
	newIndex, discrepancies, err := rec.Reconcile(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Reconciliation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d discrepancies:\n", len(discrepancies))
	for _, d := range discrepancies {
		fmt.Printf("  [%s] %s\n", d.Kind, d.Message)
	}

	// Generate snapshot
	snap := newIndex.ToSnapshot("repoctl-import")
	fmt.Printf("\nGenerated snapshot (gen=%d):\n", snap.SnapshotGen)
	fmt.Printf("  Distributions: %d\n", len(snap.Distributions))

	for _, dist := range snap.Distributions {
		pkgCount := 0
		for _, comp := range dist.Components {
			pkgCount += len(comp.Packages)
		}
		fmt.Printf("    %s: %d packages\n", dist.Suite, pkgCount)
	}

	if *apply {
		fmt.Println("\nApplying snapshot to MinIO...")
		store := minio.NewSnapshotStore(minioClient, cfg.Storage.SnapshotKey, minio.DefaultHistoryPrefix(cfg.Storage.SnapshotKey), cfg.Storage.SnapshotHistoryKeep)
		key, err := store.PutSnapshot(ctx, snap)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to persist snapshot: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Snapshot persisted to: %s\n", key)
	} else {
		fmt.Println("\nSnapshot preview generated (use --apply to persist)")
		fmt.Println("\nSnapshot JSON (first 1000 bytes):")
		data, _ := json.MarshalIndent(snap, "", "  ")
		if len(data) > 1000 {
			fmt.Println(string(data[:1000]))
			fmt.Printf("... (%d bytes total)\n", len(data))
		} else {
			fmt.Println(string(data))
		}
	}
}

func cmdReconcile(args []string) {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	configPath := fs.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
	suiteFlag := fs.String("suite", "", "Default suite for unindexed pool files (defaults to repo default_suite)")
	dryRun := fs.Bool("dry-run", false, "Show discrepancies without applying")
	apply := fs.Bool("apply", false, "Apply fixes to index")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if *dryRun && *apply {
		fmt.Println("Error: cannot use both --dry-run and --apply")
		os.Exit(1)
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Create MinIO client
	minioClient, err := minio.NewClient(minio.ClientConfig{
		Endpoint:   cfg.Storage.MinIO.Endpoint,
		AccessKey:  cfg.Storage.MinIO.AccessKey,
		SecretKey:  cfg.Storage.MinIO.SecretKey,
		Bucket:     cfg.Storage.MinIO.Bucket,
		UseTLS:     cfg.Storage.MinIO.UseTLS,
		CACertPath: cfg.Storage.MinIO.CACert,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MinIO client: %v\n", err)
		os.Exit(1)
	}

	// Create index manager
	indexMgr := index.NewManager()

	// Create reconciler
	rec := reconcile.NewReconciler(minioClient, indexMgr, defaultSuiteForConfig(cfg, *suiteFlag))

	// Reconcile (scan pool and build index)
	ctx := context.Background()
	fmt.Println("Reconciling index from pool contents...")
	newIndex, discrepancies, err := rec.Reconcile(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Reconciliation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d discrepancies:\n", len(discrepancies))
	if len(discrepancies) == 0 {
		fmt.Println("  (none)")
	}
	for _, d := range discrepancies {
		fmt.Printf("  [%s] %s\n", d.Kind, d.Message)
	}

	// Show index statistics
	fmt.Printf("\nGenerated index:\n")
	for suite, dist := range newIndex.Distributions {
		pkgCount := 0
		verCount := 0
		for _, comp := range dist.Components {
			pkgCount += len(comp.Packages)
			for _, pkg := range comp.Packages {
				verCount += len(pkg.Versions)
			}
		}
		fmt.Printf("  %s: %d packages, %d versions\n", suite, pkgCount, verCount)
	}

	if *apply {
		fmt.Println("\nApplying reconciliation...")
		indexMgr.Load(newIndex)

		// Persist snapshot
		store := minio.NewSnapshotStore(minioClient, cfg.Storage.SnapshotKey, minio.DefaultHistoryPrefix(cfg.Storage.SnapshotKey), cfg.Storage.SnapshotHistoryKeep)
		snap := newIndex.ToSnapshot("repoctl-reconcile")
		key, err := store.PutSnapshot(ctx, snap)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to persist snapshot: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Snapshot persisted to: %s\n", key)
		fmt.Println("✓ Reconciliation complete")
	} else {
		fmt.Println("\nRun with --apply to persist the reconciled index")
	}
}

func cmdUsers(args []string) {
	if len(args) < 1 {
		fmt.Println("usage: repoctl users <add|list|remove|passwd> [options]")
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		cmdUsersAdd(args[1:])
	case "list":
		cmdUsersList(args[1:])
	case "remove":
		cmdUsersRemove(args[1:])
	case "passwd":
		cmdUsersPasswd(args[1:])
	default:
		fmt.Printf("repoctl users: unknown subcommand '%s'\n", args[0])
		os.Exit(1)
	}
}

func cmdUsersAdd(args []string) {
	fs := flag.NewFlagSet("users add", flag.ExitOnError)
	configPath := fs.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
	password := fs.String("password", "", "Password (leave empty for interactive prompt)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Println("usage: repoctl users add [--config FILE] [--password PWD] <username>")
		os.Exit(1)
	}
	username := fs.Arg(0)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	minioClient, err := minio.NewClient(minio.ClientConfig{
		Endpoint:   cfg.Storage.MinIO.Endpoint,
		AccessKey:  cfg.Storage.MinIO.AccessKey,
		SecretKey:  cfg.Storage.MinIO.SecretKey,
		Bucket:     cfg.Storage.MinIO.Bucket,
		UseTLS:     cfg.Storage.MinIO.UseTLS,
		CACertPath: cfg.Storage.MinIO.CACert,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MinIO client: %v\n", err)
		os.Exit(1)
	}

	aptUserStore := minio.NewAptUserStore(minioClient, cfg.Auth.AptUsers.StoreKey)
	store, err := aptUserStore.GetUsers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load apt users: %v\n", err)
		os.Exit(1)
	}

	// Check if user already exists
	for _, u := range store.Users {
		if u.Username == username {
			fmt.Fprintf(os.Stderr, "Error: user '%s' already exists\n", username)
			os.Exit(1)
		}
	}

	// Get password
	pwd := *password
	if pwd == "" {
		fmt.Print("Password: ")
		pwdBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read password: %v\n", err)
			os.Exit(1)
		}
		pwd = string(pwdBytes)
		fmt.Println()
	}

	if pwd == "" {
		fmt.Fprintln(os.Stderr, "Error: password cannot be empty")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to hash password: %v\n", err)
		os.Exit(1)
	}

	now := time.Now().UTC()
	store.Users = append(store.Users, model.AptUser{
		Username:     username,
		PasswordHash: string(hash),
		CreatedAt:    now,
		UpdatedAt:    now,
		Disabled:     false,
	})
	store.GeneratedAt = now

	ctx := context.Background()
	key, err := aptUserStore.PutUsers(ctx, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to persist apt users: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User '%s' added and persisted to %s\n", username, key)
}

func cmdUsersList(args []string) {
	fs := flag.NewFlagSet("users list", flag.ExitOnError)
	configPath := fs.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	minioClient, err := minio.NewClient(minio.ClientConfig{
		Endpoint:   cfg.Storage.MinIO.Endpoint,
		AccessKey:  cfg.Storage.MinIO.AccessKey,
		SecretKey:  cfg.Storage.MinIO.SecretKey,
		Bucket:     cfg.Storage.MinIO.Bucket,
		UseTLS:     cfg.Storage.MinIO.UseTLS,
		CACertPath: cfg.Storage.MinIO.CACert,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MinIO client: %v\n", err)
		os.Exit(1)
	}

	aptUserStore := minio.NewAptUserStore(minioClient, cfg.Auth.AptUsers.StoreKey)
	store, err := aptUserStore.GetUsers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load apt users: %v\n", err)
		os.Exit(1)
	}

	if len(store.Users) == 0 {
		fmt.Println("No apt users found")
		return
	}

	fmt.Println("Apt users:")
	for _, u := range store.Users {
		status := "enabled"
		if u.Disabled {
			status = "disabled"
		}
		fmt.Printf("  %s [%s] (created: %s, updated: %s)\n",
			u.Username, status, u.CreatedAt.Format("2006-01-02 15:04:05"), u.UpdatedAt.Format("2006-01-02 15:04:05"))
	}
}

func cmdUsersRemove(args []string) {
	fs := flag.NewFlagSet("users remove", flag.ExitOnError)
	configPath := fs.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Println("usage: repoctl users remove [--config FILE] <username>")
		os.Exit(1)
	}
	username := fs.Arg(0)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	minioClient, err := minio.NewClient(minio.ClientConfig{
		Endpoint:   cfg.Storage.MinIO.Endpoint,
		AccessKey:  cfg.Storage.MinIO.AccessKey,
		SecretKey:  cfg.Storage.MinIO.SecretKey,
		Bucket:     cfg.Storage.MinIO.Bucket,
		UseTLS:     cfg.Storage.MinIO.UseTLS,
		CACertPath: cfg.Storage.MinIO.CACert,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MinIO client: %v\n", err)
		os.Exit(1)
	}

	aptUserStore := minio.NewAptUserStore(minioClient, cfg.Auth.AptUsers.StoreKey)
	store, err := aptUserStore.GetUsers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load apt users: %v\n", err)
		os.Exit(1)
	}

	// Find and remove user
	idx := -1
	for i, u := range store.Users {
		if u.Username == username {
			idx = i
			break
		}
	}

	if idx == -1 {
		fmt.Fprintf(os.Stderr, "Error: user '%s' not found\n", username)
		os.Exit(1)
	}

	store.Users = append(store.Users[:idx], store.Users[idx+1:]...)
	store.GeneratedAt = time.Now().UTC()

	ctx := context.Background()
	key, err := aptUserStore.PutUsers(ctx, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to persist apt users: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ User '%s' removed and persisted to %s\n", username, key)
}

func cmdUsersPasswd(args []string) {
	fs := flag.NewFlagSet("users passwd", flag.ExitOnError)
	configPath := fs.String("config", "/etc/debian-repo/config.yaml", "Path to config file")
	password := fs.String("password", "", "New password (leave empty for interactive prompt)")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Println("usage: repoctl users passwd [--config FILE] [--password PWD] <username>")
		os.Exit(1)
	}
	username := fs.Arg(0)

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	minioClient, err := minio.NewClient(minio.ClientConfig{
		Endpoint:   cfg.Storage.MinIO.Endpoint,
		AccessKey:  cfg.Storage.MinIO.AccessKey,
		SecretKey:  cfg.Storage.MinIO.SecretKey,
		Bucket:     cfg.Storage.MinIO.Bucket,
		UseTLS:     cfg.Storage.MinIO.UseTLS,
		CACertPath: cfg.Storage.MinIO.CACert,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create MinIO client: %v\n", err)
		os.Exit(1)
	}

	aptUserStore := minio.NewAptUserStore(minioClient, cfg.Auth.AptUsers.StoreKey)
	store, err := aptUserStore.GetUsers(context.Background())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load apt users: %v\n", err)
		os.Exit(1)
	}

	// Find user
	idx := -1
	for i, u := range store.Users {
		if u.Username == username {
			idx = i
			break
		}
	}

	if idx == -1 {
		fmt.Fprintf(os.Stderr, "Error: user '%s' not found\n", username)
		os.Exit(1)
	}

	// Get password
	pwd := *password
	if pwd == "" {
		fmt.Print("New password: ")
		pwdBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to read password: %v\n", err)
			os.Exit(1)
		}
		pwd = string(pwdBytes)
		fmt.Println()
	}

	if pwd == "" {
		fmt.Fprintln(os.Stderr, "Error: password cannot be empty")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to hash password: %v\n", err)
		os.Exit(1)
	}

	store.Users[idx].PasswordHash = string(hash)
	store.Users[idx].UpdatedAt = time.Now().UTC()
	store.GeneratedAt = time.Now().UTC()

	ctx := context.Background()
	key, err := aptUserStore.PutUsers(ctx, store)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to persist apt users: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Password updated for user '%s' and persisted to %s\n", username, key)
}
