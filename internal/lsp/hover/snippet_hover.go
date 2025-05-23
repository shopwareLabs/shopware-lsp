package hover

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	"github.com/shopware/shopware-lsp/internal/snippet"
	treesitterhelper "github.com/shopware/shopware-lsp/internal/tree_sitter_helper"
)

type SnippetHoverProvider struct {
	snippetIndexer *snippet.SnippetIndexer
	projectRoot    string
}

func NewSnippetHoverProvider(projectRoot string, lspServer *lsp.Server) *SnippetHoverProvider {
	snippetIndexer, _ := lspServer.GetIndexer("snippet.indexer")
	return &SnippetHoverProvider{
		snippetIndexer: snippetIndexer.(*snippet.SnippetIndexer),
		projectRoot:    projectRoot,
	}
}

func (p *SnippetHoverProvider) GetHover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if params.Node == nil {
		return nil, nil
	}

	// Handle both .twig and .php files
	switch strings.ToLower(filepath.Ext(params.TextDocument.URI)) {
	case ".twig":
		return p.twigHover(ctx, params)
	case ".php":
		return p.phpHover(ctx, params)
	default:
		return nil, nil
	}
}

func (p *SnippetHoverProvider) twigHover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if treesitterhelper.TwigTransPattern().Matches(params.Node, params.DocumentContent) {
		snippetKey := treesitterhelper.GetNodeText(params.Node, params.DocumentContent)
		return p.createHoverForSnippet(snippetKey, params)
	}
	return nil, nil
}

func (p *SnippetHoverProvider) phpHover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if treesitterhelper.IsPHPThisMethodCall("trans").Matches(params.Node, params.DocumentContent) {
		snippetKey := treesitterhelper.GetNodeText(params.Node, params.DocumentContent)
		return p.createHoverForSnippet(snippetKey, params)
	}
	return nil, nil
}

func (p *SnippetHoverProvider) createHoverForSnippet(snippetKey string, params *protocol.HoverParams) (*protocol.Hover, error) {
	snippets, err := p.snippetIndexer.GetFrontendSnippet(snippetKey)
	if err != nil || len(snippets) == 0 {
		return nil, nil
	}

	// Sort snippets by file path for consistent display
	sort.Slice(snippets, func(i, j int) bool {
		return snippets[i].File < snippets[j].File
	})

	// Build markdown content showing all translations
	var markdownContent strings.Builder
	markdownContent.WriteString(fmt.Sprintf("**Snippet**: `%s`\n\n", snippetKey))
	markdownContent.WriteString("**Translations**:\n\n")

	for _, snippet := range snippets {
		// Extract locale from file path (e.g., "de-DE" or "en-GB" from the path)
		locale := extractLocaleFromPath(snippet.File)
		
		// Make path relative to project root
		displayPath, err := filepath.Rel(p.projectRoot, snippet.File)
		if err != nil {
			displayPath = snippet.File
		}

		// Format the translation entry
		markdownContent.WriteString(fmt.Sprintf("- **%s**: `%s`\n", locale, snippet.Text))
		markdownContent.WriteString(fmt.Sprintf("  <small>%s:%d</small>\n\n", displayPath, snippet.Line))
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: markdownContent.String(),
		},
		Range: &protocol.Range{
			Start: protocol.Position{
				Line:      params.Position.Line,
				Character: params.Position.Character,
			},
			End: protocol.Position{
				Line:      params.Position.Line,
				Character: params.Position.Character + len(snippetKey),
			},
		},
	}, nil
}

// extractLocaleFromPath tries to extract the locale from the file path
// e.g., "/path/to/Resources/snippet/de-DE/snippet.json" -> "de-DE"
// e.g., "/path/to/Resources/snippet/de_DE/storefront.de-DE.json" -> "de-DE"
func extractLocaleFromPath(path string) string {
	// Normalize path separators to forward slashes for consistent handling
	// Handle both Unix and Windows path separators
	normalizedPath := strings.ReplaceAll(path, "\\", "/")
	
	// First, try to extract from filename (e.g., "storefront.de-DE.json")
	parts := strings.Split(normalizedPath, "/")
	if len(parts) > 0 {
		filename := parts[len(parts)-1]
		if strings.Contains(filename, ".") {
			filenameParts := strings.Split(filename, ".")
			for _, part := range filenameParts {
				if isLocalePattern(part) {
					return normalizeLocale(part)
				}
			}
		}
	}
	
	// Then, try to extract from directory structure
	for i, part := range parts {
		// Check if this part looks like a locale
		if isLocalePattern(part) {
			return normalizeLocale(part)
		}
		// Also check if we're in a snippet directory
		if part == "snippet" && i+1 < len(parts) {
			// The next part might be the locale
			nextPart := parts[i+1]
			if isLocalePattern(nextPart) {
				return normalizeLocale(nextPart)
			}
		}
	}
	
	return "unknown"
}

// isLocalePattern checks if a string matches common locale patterns
func isLocalePattern(s string) bool {
	// Check for patterns like "de-DE", "en-GB", "de_DE", "en_GB"
	if len(s) == 5 && (s[2] == '-' || s[2] == '_') {
		return true
	}
	// Check for patterns like "de", "en", "fr"
	if len(s) == 2 {
		return true
	}
	return false
}

// normalizeLocale converts locale to standard format (e.g., "de_DE" -> "de-DE")
func normalizeLocale(locale string) string {
	return strings.ReplaceAll(locale, "_", "-")
}