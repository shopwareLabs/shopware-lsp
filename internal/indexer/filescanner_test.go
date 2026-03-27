package indexer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestFileScanner_IndexFiles_SkipDirs(t *testing.T) {
	// Create a temporary directory for testing
	tempDir := t.TempDir()

	// Create test directory structure with files
	createTestFiles(t, tempDir)

	// Create a mock indexer that tracks which files are indexed
	mockIndexer := &mockIndexer{
		indexedFiles: make(map[string]bool),
	}

	// Create a file scanner with the mock indexer
	fs, err := NewFileScanner(tempDir, filepath.Join(tempDir, "test.db"))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, fs.Close())
	}()

	// Add the mock indexer
	fs.AddIndexer(mockIndexer)

	// Create a list of files to index
	var files []string
	err = filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".php" {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)

	// Index the files
	err = fs.IndexFiles(context.Background(), files)
	require.NoError(t, err)

	// Verify that files in excluded directories were not indexed
	for path := range mockIndexer.indexedFiles {
		relPath, err := filepath.Rel(tempDir, path)
		require.NoError(t, err)

		// Check that the file is not in any excluded directory
		pathParts := strings.Split(relPath, string(os.PathSeparator))
		for _, part := range pathParts {
			assert.False(t, defaultSkipDirs[part], "File in excluded directory was indexed: %s", path)
		}
	}

	// Verify that files in regular directories were indexed
	regularFile := filepath.Join(tempDir, "regular", "file.php")
	assert.True(t, mockIndexer.indexedFiles[regularFile], "Regular file was not indexed")

	// Verify that files in excluded directories were not indexed
	excludedFiles := []string{
		filepath.Join(tempDir, ".devenv", "file.php"),
		filepath.Join(tempDir, "node_modules", "file.php"),
		filepath.Join(tempDir, "vendor-bin", "file.php"),
		filepath.Join(tempDir, "tests", "file.php"),
		filepath.Join(tempDir, "nested", "node_modules", "file.php"),
	}

	for _, file := range excludedFiles {
		assert.False(t, mockIndexer.indexedFiles[file], "Excluded file was indexed: %s", file)
	}
}

func TestFileScanner_IndexAll_SkipDirsAtAnyDepth(t *testing.T) {
	tempDir := t.TempDir()

	createTestFiles(t, tempDir)

	mockIndexer := &mockIndexer{
		indexedFiles: make(map[string]bool),
	}

	fs, err := NewFileScanner(tempDir, filepath.Join(tempDir, "test.db"))
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, fs.Close())
	}()

	fs.AddIndexer(mockIndexer)

	err = fs.IndexAll(context.Background())
	require.NoError(t, err)

	assert.True(t, mockIndexer.indexedFiles[filepath.Join(tempDir, "regular", "file.php")], "Regular file was not indexed")
	assert.False(t, mockIndexer.indexedFiles[filepath.Join(tempDir, ".devenv", "file.php")], "Excluded file was indexed")
	assert.False(t, mockIndexer.indexedFiles[filepath.Join(tempDir, "nested", "node_modules", "file.php")], "Excluded file was indexed")
}

// Helper function to create test files
func createTestFiles(t *testing.T, baseDir string) {
	// Create directories and files for testing
	dirs := []string{
		"regular",
		".devenv",
		"node_modules",
		"vendor-bin",
		"tests",
		filepath.Join("nested", "node_modules"),
	}

	for _, dir := range dirs {
		err := os.MkdirAll(filepath.Join(baseDir, dir), 0755)
		require.NoError(t, err)

		// Create a PHP file in each directory
		filePath := filepath.Join(baseDir, dir, "file.php")
		err = os.WriteFile(filePath, []byte("<?php\n// Test file\n"), 0644)
		require.NoError(t, err)
	}
}

// Mock indexer for testing
type mockIndexer struct {
	indexedFiles map[string]bool
}

func (m *mockIndexer) Index(path string, node *tree_sitter.Node, content []byte) error {
	m.indexedFiles[path] = true
	return nil
}

func (m *mockIndexer) RemovedFiles(paths []string) error {
	for _, path := range paths {
		delete(m.indexedFiles, path)
	}
	return nil
}

func (m *mockIndexer) Name() string {
	return "mockIndexer"
}

func (m *mockIndexer) ID() string {
	return "mock"
}

func (m *mockIndexer) Close() error {
	return nil
}

func (m *mockIndexer) Clear() error {
	return nil
}
