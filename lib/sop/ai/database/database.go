package database

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	log "log/slog"

	"github.com/sharedcode/sop"
	"github.com/sharedcode/sop/ai"
	"github.com/sharedcode/sop/ai/memory"
	"github.com/sharedcode/sop/ai/model"
	"github.com/sharedcode/sop/ai/vector"
	"github.com/sharedcode/sop/btree"
	"github.com/sharedcode/sop/common"
	"github.com/sharedcode/sop/database"
	"github.com/sharedcode/sop/fs"
	"github.com/sharedcode/sop/search"
)

// RepoSource describes a repository-backed source store entry.
type RepoSource struct {
	Name    string   `json:"name,omitempty"`
	URL     string   `json:"url,omitempty"`
	Branch  string   `json:"branch,omitempty"`
	Path    string   `json:"path,omitempty"`
	Include []string `json:"include,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
}

// RepoSourceStoreManifest describes the lightweight manifest for a repository-backed source store.
type RepoSourceStoreManifest struct {
	Name            string       `json:"name,omitempty"`
	Description     string       `json:"description,omitempty"`
	Repositories    []RepoSource `json:"repositories,omitempty"`
	CredentialsRefs []string     `json:"credentials_refs,omitempty"`
}

// Database extends the core sop.Database with AI capabilities.
type Database struct {
	config sop.DatabaseOptions
	cache  sop.L2Cache
}

// NewDatabase creates a new AI-enabled database manager.
func NewDatabase(config sop.DatabaseOptions) *Database {
	config, _ = database.ValidateOptions(config)
	var to sop.TransactionOptions
	config.CopyTo(&to)
	return &Database{
		config: config,
		cache:  sop.GetL2Cache(to),
	}
}

// NewCassandraDatabase creates a new AI-enabled database manager backed by Cassandra.
func NewCassandraDatabase(config sop.DatabaseOptions) *Database {
	config, _ = database.ValidateCassandraOptions(config)
	var to sop.TransactionOptions
	config.CopyTo(&to)
	return &Database{
		config: config,
		cache:  sop.GetL2Cache(to),
	}
}

// Cache returns the L2 cache used by the database.
func (db *Database) Cache() sop.L2Cache {
	return db.cache
}

// Locker returns a Lock facet fronting the L2 cache used in the database.
// Useful for implementing Optimistic locking.
func (db *Database) Locker() sop.Locker {
	return db.cache
}

// Options returns the database options.
func (db *Database) Options() sop.DatabaseOptions {
	return db.config
}

// StoragePath returns the base storage path.
func (db *Database) StoragePath() string {
	if len(db.config.StoresFolders) > 0 {
		return db.config.StoresFolders[0]
	}
	return ""
}

// ErasureConfig returns the configured erasure coding configuration.
func (db *Database) ErasureConfig() map[string]sop.ErasureCodingConfig {
	return db.config.ErasureConfig
}

// CacheType returns the configured cache type.
func (db *Database) CacheType() sop.L2CacheType {
	return db.config.CacheType
}

// StoresFolders returns the list of storage folders (for replication).
func (db *Database) StoresFolders() []string {
	return db.config.StoresFolders
}

// BeginTransaction starts a new transaction.
func (db *Database) BeginTransaction(ctx context.Context, mode sop.TransactionMode, maxTime ...time.Duration) (sop.Transaction, error) {
	tx, err := database.BeginTransaction(ctx, db.config, mode, maxTime...)
	return tx, err
}

// StoreExists checks if a B-Tree store with the given name exists.
// It starts a read-only transaction to perform the check.
func (db *Database) StoreExists(ctx context.Context, name string) (bool, error) {
	// Optimization: For filesystem-based databases, check for the store's existence directly
	// to avoid the overhead of starting a transaction.
	if (db.config.Type == sop.Standalone || db.config.Type == sop.Clustered) && len(db.config.StoresFolders) > 0 {
		storePath := filepath.Join(db.config.StoresFolders[0], name, fs.StoreInfoFilename)
		if _, err := os.Stat(storePath); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			// Log error but fall back to transaction-based check just in case
			log.Warn("Direct filesystem check for store existence failed", "error", err)
		} else {
			return false, nil
		}
	}

	tx, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	trans, _ := tx.GetPhasedTransaction().(*common.Transaction)
	sr := trans.GetStoreRepository()

	stores, err := sr.Get(ctx, name)
	return len(stores) > 0, err
}

// Config returns the database configuration.
func (db *Database) Config() sop.DatabaseOptions {
	return db.config
}

// OpenModelStore opens a ModelStore for the specified name using the provided transaction.
func (db *Database) OpenModelStore(ctx context.Context, name string, t sop.Transaction) (ai.ModelStore, error) {
	if db.StoragePath() != "" {
		if err := os.MkdirAll(db.StoragePath(), 0755); err != nil {
			return nil, err
		}
	}
	return model.New(name, t), nil
}

// OpenBtree opens a general purpose B-Tree store with string keys and any values.
func (db *Database) OpenBtree(ctx context.Context, name string, t sop.Transaction) (btree.BtreeInterface[string, any], error) {
	if db.StoragePath() != "" {
		if err := os.MkdirAll(db.StoragePath(), 0755); err != nil {
			return nil, err
		}
	}
	return database.OpenBtree[string, any](ctx, db.config, name, t, nil)
}

// NewBtree creates a new B-Tree store with string keys and any values.
func (db *Database) NewBtree(ctx context.Context, name string, t sop.Transaction) (btree.BtreeInterface[string, any], error) {
	if db.StoragePath() != "" {
		if err := os.MkdirAll(db.StoragePath(), 0755); err != nil {
			return nil, err
		}
	}
	return database.NewBtree[string, any](ctx, db.config, name, t, nil)
}

// OpenBtreeCursor opens a cursor wrapper for a given Btree. It opens it if it is not yet.
func (db *Database) OpenBtreeCursor(ctx context.Context, name string, t sop.Transaction) (btree.BtreeInterface[string, any], error) {
	if db.StoragePath() != "" {
		if err := os.MkdirAll(db.StoragePath(), 0755); err != nil {
			return nil, err
		}
	}
	return database.OpenBtreeCursor[string, any](ctx, db.config, name, t, nil)
}

// OpenVectorStore opens a vector store with map[string]any payload.
func (db *Database) OpenVectorStore(ctx context.Context, name string, t sop.Transaction, cfg vector.Config) (ai.VectorStore[map[string]any], error) {
	if db.StoragePath() != "" {
		if err := os.MkdirAll(db.StoragePath(), 0755); err != nil {
			return nil, err
		}
	}
	// Populate replication config if available and not set in cfg
	// We access the core database's configuration via the getters we added.
	if len(cfg.TransactionOptions.StoresFolders) == 0 && len(db.StoresFolders()) > 0 {
		cfg.TransactionOptions.StoresFolders = db.StoresFolders()
	}
	if len(cfg.TransactionOptions.ErasureConfig) == 0 && len(db.ErasureConfig()) > 0 {
		cfg.TransactionOptions.ErasureConfig = db.ErasureConfig()
	}
	if cfg.TransactionOptions.CacheType == 0 {
		cfg.TransactionOptions.CacheType = db.CacheType()
	}
	if cfg.Cache == nil {
		cfg.Cache = db.Cache()
	}
	return vector.Open[map[string]any](ctx, t, name, cfg)
}

// OpenSearch opens a text search index for the specified name using the provided transaction.
func (db *Database) OpenSearch(ctx context.Context, name string, t sop.Transaction) (*search.Index, error) {
	if db.StoragePath() != "" {
		if err := os.MkdirAll(db.StoragePath(), 0755); err != nil {
			return nil, err
		}
	}
	return search.NewIndex(ctx, db.config, t, name)
}

// RemoveModelStore removes the model store and its underlying B-Tree.
func (db *Database) RemoveModelStore(ctx context.Context, name string) error {
	return database.RemoveBtree(ctx, db.config, fmt.Sprintf("%s_models", name))
}

// RemoveKnowledgeBase removes the Knowledge Base and its underlying B-Trees.
func (db *Database) RemoveKnowledgeBase(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(name), "/data") {
		name = name[:len(name)-len("/data")]
	}
	if name == "" {
		return fmt.Errorf("knowledge base name is required")
	}

	trans, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return err
	}

	stores, err := trans.GetStores(ctx)
	_ = trans.Rollback(ctx)
	if err != nil {
		return err
	}

	suggestion := ""
	for _, store := range stores {
		baseName, _, hasSlash := strings.Cut(store, "/")
		if !hasSlash {
			continue
		}
		if baseName == name {
			suggestion = ""
			break
		}
		if suggestion == "" && strings.EqualFold(baseName, name) {
			suggestion = baseName
		}
	}

	prefix := name + "/"
	var lastErr error
	removedAny := false
	for _, store := range stores {
		if !strings.HasPrefix(store, prefix) {
			continue
		}
		if err := database.RemoveBtree(ctx, db.config, store); err != nil {
			lastErr = err
			continue
		}
		removedAny = true
	}

	if removedAny {
		return nil
	}
	if lastErr == nil {
		if suggestion != "" {
			return fmt.Errorf("knowledge base '%s' not found: names are case-sensitive; did you mean '%s'?", name, suggestion)
		}
		return fmt.Errorf("knowledge base '%s' not found", name)
	}
	return lastErr
}

// ResetSemanticIndex clears persisted semantic vectors and hashes for the named knowledge base so the next rebuild can recreate them from content.
func (db *Database) ResetSemanticIndex(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("knowledge base name is required")
	}

	tx, err := db.BeginTransaction(ctx, sop.ForWriting)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	kb, err := db.OpenKnowledgeBase(ctx, name, tx, nil, nil, false)
	if err != nil {
		return err
	}
	if err := resetSemanticIndexState(ctx, kb); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func resetSemanticIndexState(ctx context.Context, kb *memory.KnowledgeBase[map[string]any]) error {
	if kb == nil {
		return nil
	}

	cfg, err := kb.GetConfig(ctx)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &memory.KnowledgeBaseConfig{}
	}
	cfg.DomainReference = nil
	if err := kb.SetConfig(ctx, cfg); err != nil {
		return err
	}
	kb.Store.SetDomainReference(nil)

	cats, err := kb.Store.Categories(ctx)
	if err != nil {
		return err
	}
	ok, err := cats.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		cat, err := cats.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		if cat != nil {
			cat.CenterVector = nil
			cat.VectorHash = ""
			cat.EmbeddingText = ""
			cat.EmbeddingVersion = ""
			if _, err := cats.UpdateCurrentValue(ctx, cat); err != nil {
				return err
			}
		}
		ok, err = cats.Next(ctx)
		if err != nil {
			return err
		}
	}

	items, err := kb.Store.Items(ctx)
	if err != nil {
		return err
	}
	vectors, err := kb.Store.Vectors(ctx)
	if err != nil {
		return err
	}
	ok, err = items.First(ctx)
	if err != nil {
		return err
	}
	for ok {
		item, err := items.GetCurrentValue(ctx)
		if err != nil {
			return err
		}
		for _, pos := range item.Positions {
			if _, err := vectors.Remove(ctx, pos); err != nil {
				return err
			}
		}
		item.Positions = nil
		item.VectorHash = ""
		item.EmbeddingText = ""
		item.EmbeddingVersion = ""
		if _, err := items.UpdateCurrentValue(ctx, item); err != nil {
			return err
		}
		ok, err = items.Next(ctx)
		if err != nil {
			return err
		}
	}

	catsByDist, err := kb.Store.CategoriesByDistance(ctx)
	if err != nil {
		return err
	}
	for {
		ok, err := catsByDist.First(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if _, err := catsByDist.RemoveCurrentItem(ctx); err != nil {
			return err
		}
	}

	for {
		ok, err := vectors.First(ctx)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if _, err := vectors.RemoveCurrentItem(ctx); err != nil {
			return err
		}
	}

	return nil
}

// RemoveVectorStore removes the vector store and its underlying B-Trees.
func (db *Database) RemoveVectorStore(ctx context.Context, name string) error {
	suffixes := []string{
		"/sys_config",
		"/tmp_vecs",
		"/data",
	}

	var lastErr error
	succeeded := false

	for _, suffix := range suffixes {
		if err := database.RemoveBtree(ctx, db.config, fmt.Sprintf("%s%s", name, suffix)); err != nil {
			lastErr = err
		}
	}

	// Also try to remove versioned tables.
	// We use an internal transaction to peek at the current version.
	trans, err := db.BeginTransaction(ctx, sop.ForReading)
	if err == nil {
		vs, err := db.OpenVectorStore(ctx, name, trans, vector.Config{})
		if err == nil {
			version, _ := vs.Version(ctx)

			f := func(versionSuffix string) int {
				i := 0
				if err := database.RemoveBtree(ctx, db.config, fmt.Sprintf("%s%s%s", name, "/lku", versionSuffix)); err == nil {
					i++
				} else {
					lastErr = err
				}
				if err := database.RemoveBtree(ctx, db.config, fmt.Sprintf("%s%s%s", name, "/centroids", versionSuffix)); err == nil {
					i++
				} else {
					lastErr = err
				}
				if err := database.RemoveBtree(ctx, db.config, fmt.Sprintf("%s%s%s", name, "/vecs", versionSuffix)); err == nil {
					i++
				} else {
					lastErr = err
				}

				log.Debug(fmt.Sprintf("version suffix %s, success count: %d", versionSuffix, i))

				return i
			}

			// Delete the unversioned current set.
			if f("") == 3 {
				succeeded = true
			}

			// Remove the current version vector components.
			versionSuffix := fmt.Sprintf("_%d", version)
			if f(versionSuffix) == 3 {
				succeeded = true
			}

			// Remove the previous version vector components, if there is.
			if version > 0 {
				versionSuffix = fmt.Sprintf("_%d", version-1)
				if f(versionSuffix) == 3 {
					succeeded = true
				}
			}

			// Remove the next version vector components, if there is.
			versionSuffix = fmt.Sprintf("_%d", version+1)
			if f(versionSuffix) == 3 {
				succeeded = true
			}
		}
		// We only read, so rollback is fine/preferred to release locks immediately.
		_ = trans.Rollback(ctx)
	}

	// We know we deleted a set of VectorStore component files, return nil to denote success.
	if succeeded {
		lastErr = nil
	}

	return lastErr
}

// RemoveSearch removes the search index and its underlying B-Trees.
func (db *Database) RemoveSearch(ctx context.Context, name string) error {
	var lastErr error
	remove := func(n string) {
		if err := database.RemoveBtree(ctx, db.config, n); err != nil {
			lastErr = err
		}
	}

	suffixes := []string{
		"_postings",
		"_term_stats",
		"_doc_stats",
		"_global",
	}

	for _, suffix := range suffixes {
		remove(fmt.Sprintf("%s%s", name, suffix))
	}
	return lastErr
}

// readStoreListFile reads a simple side-car store manifest file from the database root.
// These manifests are intentionally lightweight and non-ACID: they hold intake-only
// source-store names for folder and repository containers that can be re-digested into
// a Space later without participating in the core transactional store graph.
func (db *Database) storageFolders() []string {
	folderList := db.config.StoresFolders
	if len(folderList) == 0 && db.StoragePath() != "" {
		folderList = []string{db.StoragePath()}
	}
	if len(folderList) == 0 {
		return nil
	}
	return folderList
}

func SanitizeSourceStoreName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "source-store"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r == '/', r == '\\', r == ':', r == '*', r == '?', r == '"', r == '<', r == '>', r == '|':
			return '_'
		default:
			return r
		}
	}, name)
}

func sanitizeSourceStoreName(name string) string {
	return SanitizeSourceStoreName(name)
}

func (db *Database) writeStoreListFile(filename string, stores []string) error {
	folderList := db.storageFolders()
	if len(folderList) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(stores))
	ordered := make([]string, 0, len(stores))
	for _, store := range stores {
		name := strings.TrimSpace(store)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		ordered = append(ordered, name)
	}

	content := strings.Join(ordered, "\n")
	if content != "" {
		content += "\n"
	}
	for _, folder := range folderList {
		path := filepath.Join(folder, filename)
		if err := os.MkdirAll(folder, 0o755); err != nil {
			return fmt.Errorf("create storage folder %s: %w", folder, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

func (db *Database) readStoreListFile(filename string) ([]string, error) {
	folderList := db.storageFolders()
	if len(folderList) == 0 {
		return []string{}, nil
	}

	seen := make(map[string]struct{})
	var stores []string
	for _, folder := range folderList {
		path := filepath.Join(folder, filename)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", path, err)
		}

		for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
			name := strings.TrimSpace(line)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			stores = append(stores, name)
		}
	}

	return stores, nil
}

// GetStores returns the standard stores, excluding system and vector databases.
func (db *Database) GetStores(ctx context.Context) ([]string, error) {
	return db.getStores(ctx, false)
}

// GetFolderStores returns the folder-backed source stores declared in the side-car manifest.
func (db *Database) GetFolderStores(ctx context.Context) ([]string, error) {
	_ = ctx
	return db.readStoreListFile("folderstorelist.txt")
}

// ResolveFolderStorePath locates the on-disk directory for a folder-backed source store.
func (db *Database) ResolveFolderStorePath(ctx context.Context, name string) (string, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("source store name is required")
	}
	for _, folder := range db.storageFolders() {
		candidates := []string{filepath.Join(folder, name), filepath.Join(folder, SanitizeSourceStoreName(name))}
		for _, candidate := range candidates {
			info, err := os.Stat(candidate)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return "", fmt.Errorf("stat %s: %w", candidate, err)
			}
			if info.IsDir() {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("folder source store %q not found", name)
}

// ListFolderStoreFiles returns the relative file paths inside a folder-backed source store.
func (db *Database) ListFolderStoreFiles(ctx context.Context, name string) ([]string, error) {
	storePath, err := db.ResolveFolderStorePath(ctx, name)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.Walk(storePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(storePath, path)
		if err != nil {
			relPath = info.Name()
		}
		files = append(files, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", storePath, err)
	}
	sort.Strings(files)
	return files, nil
}

// ReadFolderStoreFile returns the text content from a file inside a folder-backed source store.
func (db *Database) ReadFolderStoreFile(ctx context.Context, name, relPath string) (string, error) {
	storePath, err := db.ResolveFolderStorePath(ctx, name)
	if err != nil {
		return "", err
	}
	cleanRelPath := strings.TrimSpace(relPath)
	if cleanRelPath == "" {
		return "", fmt.Errorf("file path is required")
	}
	fullPath := filepath.Join(storePath, filepath.FromSlash(strings.TrimPrefix(cleanRelPath, "/")))
	cleanRoot := filepath.Clean(storePath)
	cleanPath := filepath.Clean(fullPath)
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes source store root")
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file is not valid utf-8")
	}
	return string(data), nil
}

// AddFolderStore appends a folder-backed source store to the side-car manifest.
func (db *Database) AddFolderStore(ctx context.Context, name string) error {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("source store name is required")
	}

	stores, err := db.GetFolderStores(ctx)
	if err != nil {
		return err
	}
	for _, existing := range stores {
		if existing == name {
			return nil
		}
	}

	stores = append(stores, name)
	return db.writeStoreListFile("folderstorelist.txt", stores)
}

// GetRepoStores returns the repository-backed source stores declared in the side-car manifest.
func (db *Database) GetRepoStores(ctx context.Context) ([]string, error) {
	_ = ctx
	return db.readStoreListFile("repostorelist.txt")
}

// AddRepoStore appends a repository-backed source store and persists its manifest.
func (db *Database) AddRepoStore(ctx context.Context, name string, manifest RepoSourceStoreManifest) error {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("source store name is required")
	}
	manifest.Name = name

	stores, err := db.GetRepoStores(ctx)
	if err != nil {
		return err
	}
	for _, existing := range stores {
		if existing == name {
			return db.WriteRepoStoreManifest(ctx, name, manifest)
		}
	}

	stores = append(stores, name)
	if err := db.writeStoreListFile("repostorelist.txt", stores); err != nil {
		return err
	}
	return db.WriteRepoStoreManifest(ctx, name, manifest)
}

// WriteRepoStoreManifest persists the manifest for a repository-backed source store.
func (db *Database) WriteRepoStoreManifest(ctx context.Context, name string, manifest RepoSourceStoreManifest) error {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("source store name is required")
	}

	manifest.Name = name
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal repo store manifest: %w", err)
	}

	for _, folder := range db.storageFolders() {
		path := filepath.Join(folder, fmt.Sprintf("%s.repo.json", sanitizeSourceStoreName(name)))
		if err := os.MkdirAll(folder, 0o755); err != nil {
			return fmt.Errorf("create storage folder %s: %w", folder, err)
		}
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("write repo store manifest %s: %w", path, err)
		}
	}
	return nil
}

// ReadRepoStoreManifest returns the persisted manifest for a repository-backed source store.
func (db *Database) ReadRepoStoreManifest(ctx context.Context, name string) (RepoSourceStoreManifest, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return RepoSourceStoreManifest{}, fmt.Errorf("source store name is required")
	}

	for _, folder := range db.storageFolders() {
		path := filepath.Join(folder, fmt.Sprintf("%s.repo.json", sanitizeSourceStoreName(name)))
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return RepoSourceStoreManifest{}, fmt.Errorf("read repo store manifest %s: %w", path, err)
		}
		var manifest RepoSourceStoreManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return RepoSourceStoreManifest{}, fmt.Errorf("decode repo store manifest %s: %w", path, err)
		}
		manifest.Name = name
		return manifest, nil
	}

	return RepoSourceStoreManifest{Name: name}, nil
}

// ResolveRepoStoreRoot locates the repository root for a repo-backed source store.
func (db *Database) ResolveRepoStoreRoot(ctx context.Context, name string) (string, error) {
	_ = ctx
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("source store name is required")
	}
	manifest, err := db.ReadRepoStoreManifest(ctx, name)
	if err != nil {
		return "", err
	}
	if len(manifest.Repositories) == 0 {
		return "", fmt.Errorf("repo source store %q has no repositories", name)
	}
	repo := manifest.Repositories[0]
	if repo.URL == "" {
		return "", fmt.Errorf("repo source store %q has no repository URL", name)
	}
	if filepath.IsAbs(repo.URL) {
		return repo.URL, nil
	}
	for _, folder := range db.storageFolders() {
		candidate := filepath.Join(folder, repo.URL)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, nil
		}
	}
	return repo.URL, nil
}

// ListRepoStoreFiles returns the relative file paths inside a repo-backed source store.
func (db *Database) ListRepoStoreFiles(ctx context.Context, name string) ([]string, error) {
	storeRoot, err := db.ResolveRepoStoreRoot(ctx, name)
	if err != nil {
		return nil, err
	}
	var files []string
	err = filepath.Walk(storeRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(storeRoot, path)
		if err != nil {
			relPath = info.Name()
		}
		files = append(files, filepath.ToSlash(relPath))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", storeRoot, err)
	}
	sort.Strings(files)
	return files, nil
}

// ReadRepoStoreFile returns the text content from a file inside a repo-backed source store.
func (db *Database) ReadRepoStoreFile(ctx context.Context, name, relPath string) (string, error) {
	storeRoot, err := db.ResolveRepoStoreRoot(ctx, name)
	if err != nil {
		return "", err
	}
	cleanRelPath := strings.TrimSpace(relPath)
	if cleanRelPath == "" {
		return "", fmt.Errorf("file path is required")
	}
	fullPath := filepath.Join(storeRoot, filepath.FromSlash(strings.TrimPrefix(cleanRelPath, "/")))
	cleanRoot := filepath.Clean(storeRoot)
	cleanPath := filepath.Clean(fullPath)
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes source store root")
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("file is not valid utf-8")
	}
	return string(data), nil
}

// GetDomains returns only the vector databases (domains).
func (db *Database) GetDomains(ctx context.Context) ([]string, error) {
	return db.getStores(ctx, true)
}

func (db *Database) getStores(ctx context.Context, vector bool) ([]string, error) {
	trans, err := db.BeginTransaction(ctx, sop.ForReading)
	if err != nil {
		return nil, err
	}
	defer trans.Rollback(ctx)

	if getStoresTx, ok := trans.(interface {
		GetStores(context.Context) ([]string, error)
	}); ok {
		allStores, err := getStoresTx.GetStores(ctx)
		if err == nil {
			var stores []string
			kbLookup := make(map[string]byte)
			for _, store := range allStores {
				if !vector {
					if !strings.Contains(store, "/") {
						stores = append(stores, store)
					}
				} else {
					if strings.Contains(store, "/") {
						parts := strings.Split(store, "/")
						if _, exists := kbLookup[parts[0]]; !exists {
							stores = append(stores, parts[0])
							kbLookup[parts[0]] = 1
						}
					}
				}
			}
			if len(stores) > 0 || vector {
				return stores, nil
			}
		}
	}

	if stores, fileErr := db.readStoreListFile("storelist.txt"); fileErr == nil {
		if len(stores) > 0 {
			return stores, nil
		}
		return make([]string, 0), nil
	}

	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("underlying transaction does not support GetStores")
}
