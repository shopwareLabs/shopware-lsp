package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	_ "modernc.org/sqlite"
)

var defaultSkipDirs = map[string]bool{
	"node_modules": true,
	"var":          true,
	"vendor-bin":   true,
	"bin":          true,
	"cache":        true,
	"dist":         true,
	".git":         true,
	".github":      true,
	".gitlab":      true,
	".run":         true,
	".idea":        true,
	".vscode":      true,
	"tests":        true,
	"public":       true,
	".devenv":      true,
	".direnv":      true,
}

// FileScanner scans the project for files and tracks changes
type FileScanner struct {
	projectRoot string
	db          *sql.DB
	indexer     []Indexer
	watcher     *fsnotify.Watcher
	watcherCtx  context.Context
	cancel      context.CancelFunc
	watcherWg   sync.WaitGroup
	onUpdate    func()
}

// NewFileScanner creates a new file scanner
func NewFileScanner(projectRoot string, dbPath string) (*FileScanner, error) {
	// Ensure parent directory exists for the DB file
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("failed to create db directory: %w", err)
	}

	// Open the database with WAL mode for concurrent access
	// Using _txlock=immediate to acquire locks early and avoid SQLITE_BUSY
	db, err := sql.Open("sqlite", dbPath+"?_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Enable WAL mode and set pragmas for concurrent access and optimization
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=10000",
		"PRAGMA auto_vacuum=INCREMENTAL",
		"PRAGMA wal_autocheckpoint=1000",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to set pragma %s: %w", pragma, err)
		}
	}

	// Create the table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS file_hashes (
			path TEXT PRIMARY KEY,
			size INTEGER NOT NULL,
			mtime INTEGER NOT NULL
		)
	`)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	// Create a new context for the watcher
	ctx, cancel := context.WithCancel(context.Background())

	return &FileScanner{
		projectRoot: projectRoot,
		db:          db,
		indexer:     []Indexer{},
		watcherCtx:  ctx,
		cancel:      cancel,
	}, nil
}

func (fs *FileScanner) SetOnUpdate(onUpdate func()) {
	fs.onUpdate = onUpdate
}

func (fs *FileScanner) AddIndexer(indexer Indexer) {
	fs.indexer = append(fs.indexer, indexer)
}

func shouldSkipRelPath(relPath string) bool {
	if relPath == "" || relPath == "." {
		return false
	}

	pathParts := strings.Split(relPath, string(os.PathSeparator))
	for _, part := range pathParts {
		if defaultSkipDirs[part] {
			return true
		}
	}

	return false
}

// StartWatcher starts watching for file changes in the project directory
func (fs *FileScanner) StartWatcher() error {
	// Create a new watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	fs.watcher = watcher
	fs.watcherWg.Add(1)

	// Start the watcher goroutine
	go func() {
		defer fs.watcherWg.Done()
		defer func() {
			if fs.watcher != nil {
				_ = fs.watcher.Close()
			}
		}()

		// Use a debounce mechanism to avoid processing the same file multiple times
		pendingAdds := make(map[string]bool)
		pendingRemoves := make(map[string]bool)
		debounceTimer := time.NewTimer(time.Hour) // Initialize with a long duration
		debounceTimer.Stop()                      // Stop it immediately

		processChanges := func() {
			// Process adds/modifications
			if len(pendingAdds) > 0 {
				filesToAdd := make([]string, 0, len(pendingAdds))
				for file := range pendingAdds {
					filesToAdd = append(filesToAdd, file)
				}
				pendingAdds = make(map[string]bool)

				log.Printf("Processing %d changed/added files", len(filesToAdd))
				if err := fs.IndexFiles(fs.watcherCtx, filesToAdd); err != nil {
					log.Printf("Error indexing files: %v", err)
				}
			}

			// Process removes
			if len(pendingRemoves) > 0 {
				filesToRemove := make([]string, 0, len(pendingRemoves))
				for file := range pendingRemoves {
					filesToRemove = append(filesToRemove, file)
				}
				pendingRemoves = make(map[string]bool)

				log.Printf("Processing %d deleted files", len(filesToRemove))
				if err := fs.RemoveFiles(fs.watcherCtx, filesToRemove); err != nil {
					log.Printf("Error removing files: %v", err)
				}
			}
		}

		for {
			select {
			case <-fs.watcherCtx.Done():
				// Process any pending changes before exiting
				processChanges()
				return

			case event, ok := <-fs.watcher.Events:
				if !ok {
					return
				}

				// Skip directories that should be ignored
				relPath, err := filepath.Rel(fs.projectRoot, event.Name)
				if err == nil && shouldSkipRelPath(relPath) {
					continue
				}

				// Get file info
				fileInfo, err := os.Stat(event.Name)
				if err != nil {
					// File might have been deleted
					if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
						// Check if it's a file type we care about
						ext := strings.ToLower(filepath.Ext(event.Name))
						if slices.Contains(scannedFileTypes, ext) {
							pendingRemoves[event.Name] = true
							// Reset the debounce timer
							if !debounceTimer.Stop() {
								select {
								case <-debounceTimer.C:
								default:
								}
							}
							debounceTimer.Reset(200 * time.Millisecond)
						}
					}
					continue
				}

				// Skip directories
				if fileInfo.IsDir() {
					// If a directory is created, add it to the watcher
					if event.Op&fsnotify.Create != 0 {
						if err := fs.addDirectoryToWatcher(event.Name); err != nil {
							log.Printf("Error adding directory to watcher: %v", err)
						}
					}
					continue
				}

				// Handle file events
				ext := strings.ToLower(filepath.Ext(event.Name))
				if slices.Contains(scannedFileTypes, ext) {
					if event.Op&(fsnotify.Create|fsnotify.Write) != 0 {
						// File was created or modified
						if event.Op&fsnotify.Create != 0 {
							log.Printf("File created: %s", event.Name)
						} else {
							log.Printf("File modified: %s", event.Name)
						}
						pendingAdds[event.Name] = true
						// Remove from pending removes if it was there
						delete(pendingRemoves, event.Name)
					} else if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
						// File was removed or renamed
						if event.Op&fsnotify.Remove != 0 {
							log.Printf("File removed: %s", event.Name)
						} else {
							log.Printf("File renamed: %s", event.Name)
						}
						pendingRemoves[event.Name] = true
						// Remove from pending adds if it was there
						delete(pendingAdds, event.Name)
					}

					// Reset the debounce timer
					if !debounceTimer.Stop() {
						select {
						case <-debounceTimer.C:
						default:
						}
					}
					debounceTimer.Reset(200 * time.Millisecond)
				}

			case err, ok := <-fs.watcher.Errors:
				if !ok {
					return
				}
				log.Printf("File watcher error: %v", err)

			case <-debounceTimer.C:
				// Process changes after the debounce period
				processChanges()
			}
		}
	}()

	// Add the project root directory to the watcher
	return fs.addDirectoryToWatcher(fs.projectRoot)
}

// StopWatcher stops the file watcher
func (fs *FileScanner) StopWatcher() {
	if fs.watcher != nil {
		// Cancel the context to signal the watcher goroutine to stop
		fs.cancel()

		// Wait for the watcher goroutine to finish
		fs.watcherWg.Wait()

		// Reset the watcher
		fs.watcher = nil
	}
}

// addDirectoryToWatcher recursively adds a directory and its subdirectories to the watcher
func (fs *FileScanner) addDirectoryToWatcher(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files/dirs we can't access
		}

		// Only watch directories
		if !info.IsDir() {
			return nil
		}

		// Skip directories in the skipDirs list
		relPath, err := filepath.Rel(fs.projectRoot, path)
		if err == nil && shouldSkipRelPath(relPath) {
			return filepath.SkipDir
		}

		// Add the directory to the watcher
		if err := fs.watcher.Add(path); err != nil {
			log.Printf("Error watching directory %s: %v", path, err)
		}

		return nil
	})
}

// Close closes the database and stops the file watcher
func (fs *FileScanner) Close() error {
	// Stop the file watcher if it's running
	if fs.watcher != nil {
		fs.StopWatcher()
	}

	// Close all indexers
	for _, indexer := range fs.indexer {
		if err := indexer.Close(); err != nil {
			return err
		}
	}

	// Close the database with optimization
	if fs.db != nil {
		// Optimize query planner statistics before closing
		_, _ = fs.db.Exec("PRAGMA optimize")

		// Reclaim any remaining unused space
		_, _ = fs.db.Exec("PRAGMA incremental_vacuum")

		// Checkpoint and truncate the WAL file to reduce disk usage
		_, _ = fs.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")

		return fs.db.Close()
	}

	return nil
}

func (fs *FileScanner) IndexAll(ctx context.Context) error {
	var files []string

	err := filepath.Walk(fs.projectRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		// Skip directories
		if info.IsDir() {
			// Skip directories in the skipDirs list
			relPath, err := filepath.Rel(fs.projectRoot, path)
			if err == nil && shouldSkipRelPath(relPath) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip phar files
		if strings.HasSuffix(path, ".phar.php") {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if slices.Contains(scannedFileTypes, ext) {
			files = append(files, path)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to walk project directory: %w", err)
	}

	log.Printf("Found %d files to index", len(files))

	startTime := time.Now()

	if err := fs.IndexFiles(ctx, files); err != nil {
		return fmt.Errorf("failed to index files: %w", err)
	}

	log.Printf("Indexing took %s", time.Since(startTime))

	return nil
}

// fileNeedsIndexing checks if a file needs to be indexed
func (fs *FileScanner) fileNeedsIndexing(path string) (bool, []byte, os.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, nil, nil, err
	}

	var storedSize, storedMtime int64
	err = fs.db.QueryRow("SELECT size, mtime FROM file_hashes WHERE path = ?", path).Scan(&storedSize, &storedMtime)

	fileChanged := false
	if err == sql.ErrNoRows {
		fileChanged = true
	} else if err != nil {
		fileChanged = true
	} else {
		fileChanged = storedSize != info.Size() || storedMtime != info.ModTime().UnixNano()
	}

	if !fileChanged {
		return false, nil, info, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return false, nil, info, err
	}

	return true, content, info, nil
}

// RemoveFiles removes multiple files from the index
func (fs *FileScanner) RemoveFiles(ctx context.Context, paths []string) error {
	for _, indexer := range fs.indexer {
		if err := indexer.RemovedFiles(paths); err != nil {
			return err
		}
	}

	tx, err := fs.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("DELETE FROM file_hashes WHERE path = ?")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, path := range paths {
		if _, err := stmt.Exec(path); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	if fs.onUpdate != nil {
		fs.onUpdate()
	}

	return nil
}

func (fs *FileScanner) removeFilesFromIndexers(paths []string) error {
	for _, indexer := range fs.indexer {
		if err := indexer.RemovedFiles(paths); err != nil {
			return err
		}
	}
	return nil
}

func (fs *FileScanner) updateFileStates(files []fileState) error {
	tx, err := fs.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("INSERT OR REPLACE INTO file_hashes (path, size, mtime) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, file := range files {
		if _, err := stmt.Exec(file.path, file.info.Size(), file.info.ModTime().UnixNano()); err != nil {
			return err
		}
	}

	return tx.Commit()
}

type fileState struct {
	path string
	info os.FileInfo
}

type fileWork struct {
	path    string
	content []byte
	info    os.FileInfo
}

// IndexFiles processes multiple files in parallel
func (fs *FileScanner) IndexFiles(ctx context.Context, files []string) error {
	if len(files) == 0 {
		return nil
	}

	// Filter out files in directories that should be skipped
	filteredFiles := make([]string, 0, len(files))
	for _, path := range files {
		// Get the relative path from project root
		relPath, err := filepath.Rel(fs.projectRoot, path)
		if err != nil {
			// If we can't get the relative path, keep the file to be safe
			filteredFiles = append(filteredFiles, path)
			continue
		}

		if !shouldSkipRelPath(relPath) {
			filteredFiles = append(filteredFiles, path)
		}
	}

	// Update files to only include filtered files
	files = filteredFiles

	// Determine the number of worker goroutines to use
	workerCount := runtime.NumCPU() + 2
	if workerCount > 16 {
		workerCount = 16
	}

	// Create a channel to distribute work
	fileChan := make(chan string, 100)

	// Create a channel for errors
	errChan := make(chan error, len(files))

	// Create a wait group to wait for all workers to finish
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			parsers := CreateTreesitterParsers()
			const batchSize = 50
			batch := make([]fileWork, 0, batchSize)

			processBatch := func(items []fileWork) {
				if len(items) == 0 {
					return
				}

				paths := make([]string, 0, len(items))
				for _, item := range items {
					paths = append(paths, item.path)
				}

				if err := fs.removeFilesFromIndexers(paths); err != nil {
					errChan <- err
					return
				}

				for _, item := range items {
					ext := strings.ToLower(filepath.Ext(item.path))
					parser := parsers[ext]
					if parser == nil {
						panic(fmt.Sprintf("no parser found for file type: %s", ext))
					}

					tree := parser.Parse(item.content, nil)

					for _, indexer := range fs.indexer {
						if err := indexer.Index(item.path, tree.RootNode(), item.content); err != nil {
							errChan <- err
						}
					}

					tree.Close()
				}

				fileStates := make([]fileState, 0, len(items))
				for _, item := range items {
					fileStates = append(fileStates, fileState{
						path: item.path,
						info: item.info,
					})
				}

				if err := fs.updateFileStates(fileStates); err != nil {
					errChan <- err
				}
			}

			for path := range fileChan {
				// Check if file needs indexing
				needsIndexing, content, info, err := fs.fileNeedsIndexing(path)
				if err != nil {
					// We'll just skip file errors to reduce noise
					continue
				}

				// If file hasn't changed, skip it
				if !needsIndexing {
					continue
				}

				batch = append(batch, fileWork{
					path:    path,
					content: content,
					info:    info,
				})
				if len(batch) >= batchSize {
					processBatch(batch)
					batch = batch[:0]
				}
			}

			processBatch(batch)

			CloseTreesitterParsers(parsers)
		}()
	}

	// Send files to workers
	for _, path := range files {
		fileChan <- path
	}
	close(fileChan)

	// Wait for all workers to finish
	wg.Wait()
	close(errChan)

	// Check if there were any errors
	for err := range errChan {
		log.Printf("Error processing file: %v", err)
	}

	if fs.onUpdate != nil {
		fs.onUpdate()
	}

	return nil
}

// ClearHashes clears all file hashes, forcing reindexing
func (fs *FileScanner) ClearHashes() error {
	for _, indexer := range fs.indexer {
		if err := indexer.Clear(); err != nil {
			return err
		}
	}

	_, err := fs.db.Exec("DELETE FROM file_hashes")
	if err != nil {
		return err
	}

	// Reclaim space after clearing all data
	_, err = fs.db.Exec("PRAGMA incremental_vacuum")
	return err
}
