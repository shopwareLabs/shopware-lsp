package diagnostics

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/shopware/shopware-lsp/internal/indexer"
	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	tree_sitter_twig "github.com/shopware/shopware-lsp/internal/tree_sitter_grammars/twig/bindings/go"
	"github.com/shopware/shopware-lsp/internal/twig"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestTwigVersioningDiagnosticsProvider_originalNotFoundMessage(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	fileScanner, err := indexer.NewFileScanner(tempDir, filepath.Join(tempDir, "scanner.db"))
	require.NoError(t, err)

	server := lsp.NewServer(fileScanner, tempDir, "test")
	twigIndexer, err := twig.NewTwigIndexer(tempDir)
	require.NoError(t, err)
	server.RegisterIndexer(twigIndexer, nil)

	provider := NewTwigVersioningDiagnosticsProvider(server)

	uri := "file:///tmp/myext/Resources/views/storefront/page/checkout/foo.html.twig"
	content := []byte(`{% sw_extends '@Storefront/storefront/page/checkout/foo' %}{# shopware-block: abc123def456@6.4.15.0 #}{% block content %}test{% endblock %}`)

	parser := tree_sitter.NewParser()
	lang := tree_sitter.NewLanguage(tree_sitter_twig.Language())
	require.NoError(t, parser.SetLanguage(lang))
	tree := parser.Parse(content, nil)
	defer tree.Close()

	diagnostics, err := provider.GetDiagnostics(ctx, uri, tree.RootNode(), content)
	require.NoError(t, err)

	require.Len(t, diagnostics, 1)
	assert.Contains(t, diagnostics[0].Message, "Original block not found in Storefront for block 'content'")
	assert.Equal(t, protocol.DiagnosticSeverityWarning, diagnostics[0].Severity)
	assert.Equal(t, "shopware-lsp", diagnostics[0].Source)
}

func TestTwigVersioningDiagnosticsProvider_nilIndexerNoPanic(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	fileScanner, err := indexer.NewFileScanner(tempDir, filepath.Join(tempDir, "scanner.db"))
	require.NoError(t, err)

	server := lsp.NewServer(fileScanner, tempDir, "test")

	provider := NewTwigVersioningDiagnosticsProvider(server)
	require.NotNil(t, provider)

	content := []byte(`{% block foo %}{% endblock %}`)
	uri := "file:///tmp/ext/Resources/views/storefront/page/bar.html.twig"
	parser := tree_sitter.NewParser()
	lang := tree_sitter.NewLanguage(tree_sitter_twig.Language())
	require.NoError(t, parser.SetLanguage(lang))
	tree := parser.Parse(content, nil)
	defer tree.Close()

	diagnostics, err := provider.GetDiagnostics(ctx, uri, tree.RootNode(), content)
	require.NoError(t, err)
	assert.Empty(t, diagnostics)
}
