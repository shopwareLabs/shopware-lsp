package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/shopware/shopware-lsp/internal/admin"
	"github.com/shopware/shopware-lsp/internal/extension"
	"github.com/shopware/shopware-lsp/internal/feature"
	"github.com/shopware/shopware-lsp/internal/indexer"
	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/codeaction"
	"github.com/shopware/shopware-lsp/internal/lsp/codelens"
	"github.com/shopware/shopware-lsp/internal/lsp/completion"
	"github.com/shopware/shopware-lsp/internal/lsp/definition"
	"github.com/shopware/shopware-lsp/internal/lsp/diagnostics"
	"github.com/shopware/shopware-lsp/internal/lsp/hover"
	"github.com/shopware/shopware-lsp/internal/lsp/reference"
	"github.com/shopware/shopware-lsp/internal/php"
	"github.com/shopware/shopware-lsp/internal/snippet"
	"github.com/shopware/shopware-lsp/internal/symfony"
	"github.com/shopware/shopware-lsp/internal/systemconfig"
	"github.com/shopware/shopware-lsp/internal/theme"
	"github.com/shopware/shopware-lsp/internal/twig"
)

// Version is set during build by goreleaser
var version = "dev"

func main() {
	log.SetFlags(0)

	// Get the current working directory as project root
	projectRoot, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get working directory: %v", err)
	}

	cacheDir, err := getProjectCacheFolder(projectRoot)
	if err != nil {
		log.Fatalf("Failed to get project config directory: %v", err)
	}

	log.Printf("Using cache directory: %s", cacheDir)
	log.Printf("Shopware LSP version: %s", version)

	// Check cache version and migrate if needed
	cacheCleared, err := indexer.CheckAndMigrateCache(cacheDir)
	if err != nil {
		log.Fatalf("Failed to check/migrate cache: %v", err)
	}
	if cacheCleared {
		log.Printf("Cache version mismatch - cleared old cache (new version: %d)", indexer.IndexVersion)
	}

	filescanner, err := indexer.NewFileScanner(projectRoot, filepath.Join(cacheDir, "file_scanner.db"))
	if err != nil {
		log.Fatalf("Failed to create file scanner: %v", err)
	}

	server := lsp.NewServer(filescanner, cacheDir, version)

	server.RegisterIndexer(symfony.NewServiceIndex(projectRoot, cacheDir))
	server.RegisterIndexer(symfony.NewRouteIndexer(cacheDir))
	server.RegisterIndexer(symfony.NewRouteUsageIndexer(cacheDir))
	server.RegisterIndexer(php.NewPHPIndex(cacheDir))
	server.RegisterIndexer(twig.NewTwigIndexer(cacheDir))
	server.RegisterIndexer(snippet.NewSnippetIndexer(cacheDir))
	server.RegisterIndexer(feature.NewFeatureIndexer(cacheDir))
	server.RegisterIndexer(systemconfig.NewSystemConfigIndexer(cacheDir))
	server.RegisterIndexer(theme.NewThemeConfigIndexer(cacheDir))
	server.RegisterIndexer(extension.NewExtensionIndexer(cacheDir))
	server.RegisterIndexer(admin.NewAdminComponentIndexer(cacheDir))

	server.RegisterCompletionProvider(completion.NewServiceCompletionProvider(server))
	server.RegisterCompletionProvider(completion.NewTwigCompletionProvider(projectRoot, server))
	server.RegisterCompletionProvider(completion.NewRouteCompletionProvider(server))
	server.RegisterCompletionProvider(completion.NewSnippetCompletionProvider(server))
	server.RegisterCompletionProvider(completion.NewFeatureCompletionProvider(server))
	server.RegisterCompletionProvider(completion.NewSystemConfigCompletion(server))
	server.RegisterCompletionProvider(completion.NewThemeCompletionProvider(server))
	server.RegisterCompletionProvider(completion.NewAdminCompletionProvider(server))

	server.RegisterDefinitionProvider(definition.NewServiceXMLDefinitionProvider(server))
	server.RegisterDefinitionProvider(definition.NewTwigDefinitionProvider(projectRoot, server))
	server.RegisterDefinitionProvider(definition.NewRouteDefinitionProvider(server))
	server.RegisterDefinitionProvider(definition.NewSnippetDefinitionProvider(server))
	server.RegisterDefinitionProvider(definition.NewFeatureDefinitionProvider(server))
	server.RegisterDefinitionProvider(definition.NewSystemConfigDefinitionProvider(server))
	server.RegisterDefinitionProvider(definition.NewThemeDefinitionProvider(server))
	server.RegisterDefinitionProvider(definition.NewAdminDefinitionProvider(server))

	server.RegisterCodeLensProvider(codelens.NewPHPCodeLensProvider(server))
	server.RegisterCodeLensProvider(codelens.NewTwigCodeLensProvider(server))

	server.RegisterReferencesProvider(reference.NewRouteReferenceProvider(server))

	server.RegisterDiagnosticsProvider(diagnostics.NewSnippetDiagnosticsProvider(server))
	server.RegisterDiagnosticsProvider(diagnostics.NewThemeDiagnosticsProvider(projectRoot, server))
	server.RegisterDiagnosticsProvider(diagnostics.NewTwigVersioningDiagnosticsProvider(server))
	server.RegisterDiagnosticsProvider(diagnostics.NewAdminDiagnosticsProvider(server))

	// Register hover providers
	server.RegisterHoverProvider(hover.NewTwigHoverProvider(projectRoot, server))
	server.RegisterHoverProvider(hover.NewSnippetHoverProvider(projectRoot, server))
	server.RegisterHoverProvider(hover.NewTwigVersioningHoverProvider(server))
	server.RegisterHoverProvider(hover.NewAdminHoverProvider(projectRoot, server))

	// Register code action providers
	server.RegisterCodeActionProvider(codeaction.NewSnippetCodeActionProvider(server))
	server.RegisterCodeActionProvider(codeaction.NewTwigCodeActionProvider(projectRoot, server))
	server.RegisterCodeActionProvider(codeaction.NewAdminCodeActionProvider(server))

	server.RegisterCommandProvider(snippet.NewSnippetCommandProvider(server))
	server.RegisterCommandProvider(extension.NewExtensionCommandProvider(server))
	server.RegisterCommandProvider(twig.NewTwigCommandProvider(projectRoot, server))

	if err := server.Start(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("LSP server error: %v", err)
	}
}
