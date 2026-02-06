package hover

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopware/shopware-lsp/internal/indexer"
	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	tree_sitter_twig "github.com/shopware/shopware-lsp/internal/tree_sitter_grammars/twig/bindings/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestTwigVersioningHoverProvider_nilIndexerNoPanic(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	fileScanner, err := indexer.NewFileScanner(tempDir, filepath.Join(tempDir, "scanner.db"))
	require.NoError(t, err)

	server := lsp.NewServer(fileScanner, tempDir, "test")

	provider := NewTwigVersioningHoverProvider(server)
	require.NotNil(t, provider)

	content := []byte(`{% block foo %}{% endblock %}`)
	parser := tree_sitter.NewParser()
	lang := tree_sitter.NewLanguage(tree_sitter_twig.Language())
	require.NoError(t, parser.SetLanguage(lang))
	tree := parser.Parse(content, nil)
	defer tree.Close()

	params := &protocol.HoverParams{
		TextDocument: struct {
			URI string `json:"uri"`
		}{URI: "file:///tmp/foo.twig"},
		Position:        protocol.Position{Line: 0, Character: 10},
		DocumentContent: content,
		Node:            tree.RootNode(),
	}

	hover, err := provider.GetHover(ctx, params)
	require.NoError(t, err)
	assert.Nil(t, hover)
}
