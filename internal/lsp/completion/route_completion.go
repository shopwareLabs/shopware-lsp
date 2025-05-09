package completion

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	"github.com/shopware/shopware-lsp/internal/symfony"
	treesitterhelper "github.com/shopware/shopware-lsp/internal/tree_sitter_helper"
)

type RouteCompletionProvider struct {
	routeIndex *symfony.RouteIndexer
}

func NewRouteCompletionProvider(server *lsp.Server) *RouteCompletionProvider {
	routeIndexer, _ := server.GetIndexer("symfony.route")
	return &RouteCompletionProvider{
		routeIndex: routeIndexer.(*symfony.RouteIndexer),
	}
}

func (p *RouteCompletionProvider) GetCompletions(ctx context.Context, params *protocol.CompletionParams) []protocol.CompletionItem {
	if params.Node == nil {
		return []protocol.CompletionItem{}
	}

	switch strings.ToLower(filepath.Ext(params.TextDocument.URI)) {
	case ".php":
		return p.phpCompletions(ctx, params)
	case ".twig":
		return p.twigCompletions(ctx, params)
	default:
		return []protocol.CompletionItem{}
	}
}

func (p *RouteCompletionProvider) phpCompletions(ctx context.Context, params *protocol.CompletionParams) []protocol.CompletionItem {
	if treesitterhelper.IsPHPThisMethodCall("redirectToRoute").Matches(params.Node, params.DocumentContent) {
		allRoutes, _ := p.routeIndex.GetRoutes()

		var completionItems []protocol.CompletionItem
		for _, route := range allRoutes {
			completionItems = append(completionItems, protocol.CompletionItem{
				Label: route.Name,
			})
		}

		return completionItems
	}

	return []protocol.CompletionItem{}
}

func (p *RouteCompletionProvider) twigCompletions(ctx context.Context, params *protocol.CompletionParams) []protocol.CompletionItem {
	if treesitterhelper.TwigStringInFunctionPattern("seoUrl", "url", "path").Matches(params.Node, []byte(params.DocumentContent)) {
		routes, _ := p.routeIndex.GetRoutes()

		var completionItems []protocol.CompletionItem
		for _, route := range routes {
			completionItems = append(completionItems, protocol.CompletionItem{
				Label: route.Name,
			})
		}

		return completionItems
	}

	return []protocol.CompletionItem{}
}

func (p *RouteCompletionProvider) GetTriggerCharacters() []string {
	return []string{}
}
