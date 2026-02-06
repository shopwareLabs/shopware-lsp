package hover

import (
	"context"
	"fmt"
	"strings"

	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	"github.com/shopware/shopware-lsp/internal/twig"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type TwigVersioningHoverProvider struct {
	twigIndexer *twig.TwigIndexer
}

func NewTwigVersioningHoverProvider(lspServer *lsp.Server) *TwigVersioningHoverProvider {
	indexer, ok := lspServer.GetIndexer("twig.indexer")
	if !ok {
		return &TwigVersioningHoverProvider{twigIndexer: nil}
	}
	twigIndexer, ok := indexer.(*twig.TwigIndexer)
	if !ok {
		return &TwigVersioningHoverProvider{twigIndexer: nil}
	}
	return &TwigVersioningHoverProvider{twigIndexer: twigIndexer}
}

func (p *TwigVersioningHoverProvider) GetHover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if !strings.HasSuffix(strings.ToLower(params.TextDocument.URI), ".twig") {
		return nil, nil
	}

	if params.Node == nil {
		return nil, nil
	}

	if p.twigIndexer == nil {
		return nil, nil
	}

	if p.isBlockIdentifier(params.Node, string(params.DocumentContent)) {
		return p.hoverBlockIdentifier(params.Node, string(params.DocumentContent), params.TextDocument.URI)
	}

	if p.isVersionComment(params.Node, string(params.DocumentContent)) {
		return p.hoverVersionComment(params.Node, string(params.DocumentContent), params.TextDocument.URI)
	}

	return nil, nil
}

func (p *TwigVersioningHoverProvider) isBlockIdentifier(node *tree_sitter.Node, content string) bool {
	if node.Kind() != "identifier" {
		return false
	}

	parent := node.Parent()
	return parent != nil && parent.Kind() == "block"
}

func (p *TwigVersioningHoverProvider) isVersionComment(node *tree_sitter.Node, content string) bool {
	if node.Kind() != "comment" {
		return false
	}

	commentText := string(node.Utf8Text([]byte(content)))
	return strings.Contains(commentText, twig.VersionCommentPrefix)
}

func (p *TwigVersioningHoverProvider) hoverBlockIdentifier(node *tree_sitter.Node, content string, uri string) (*protocol.Hover, error) {
	blockName := string(node.Utf8Text([]byte(content)))

	allBlockHashes, err := p.twigIndexer.GetTwigBlockHashes(blockName)
	if err != nil {
		return nil, err
	}

	originalHash := twig.FindOriginalStorefrontHash(allBlockHashes)

	var hoverText strings.Builder
	hoverText.WriteString(fmt.Sprintf("**Block:** `%s`\n\n", blockName))

	if originalHash != nil {
		hoverText.WriteString(fmt.Sprintf("**Original Hash:** `%s`\n\n", originalHash.Hash))
		hoverText.WriteString(fmt.Sprintf("**Template Path:** `%s`\n\n", originalHash.RelativePath))

		twigFiles, err := p.twigIndexer.GetTwigFilesByRelPath(twig.ConvertToRelativePath(uri))
		if err == nil && len(twigFiles) > 0 {
			if block, exists := twigFiles[0].Blocks[blockName]; exists && block.VersionComment != nil {
				if block.VersionComment.Hash == originalHash.Hash {
					hoverText.WriteString("**Status:** Block version is up to date\n\n")
				} else {
					hoverText.WriteString("**Status:** Block version is outdated\n\n")
					hoverText.WriteString(fmt.Sprintf("**Current Version:** `%s`\n\n", block.VersionComment.Version))
				}
			} else {
				hoverText.WriteString("❗ **Status:** No version comment found\n\n")
			}
		}
	} else {
		hoverText.WriteString("ℹ️ **Status:** No original block found in Storefront templates\n\n")
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: hoverText.String(),
		},
	}, nil
}

func (p *TwigVersioningHoverProvider) hoverVersionComment(node *tree_sitter.Node, content string, uri string) (*protocol.Hover, error) {
	commentText := string(node.Utf8Text([]byte(content)))

	versionComment := twig.ParseVersionComment(commentText, int(node.Range().StartPoint.Row)+1)
	if versionComment == nil {
		return nil, nil
	}

	var hoverText strings.Builder
	hoverText.WriteString("**Shopware Block Version Comment**\n\n")
	hoverText.WriteString(fmt.Sprintf("**Hash:** `%s`\n\n", versionComment.Hash))
	hoverText.WriteString(fmt.Sprintf("**Version:** `%s`\n\n", versionComment.Version))

	blockName := p.findBlockNameAfterComment(node, content)
	if blockName != "" {
		allBlockHashes, err := p.twigIndexer.GetTwigBlockHashes(blockName)
		if err == nil {
			originalHash := twig.FindOriginalStorefrontHash(allBlockHashes)
			if originalHash != nil {
				if versionComment.Hash == originalHash.Hash {
					hoverText.WriteString("**Status:** Version comment matches original block\n\n")
				} else {
					hoverText.WriteString("**Status:** Version comment is outdated\n\n")
					hoverText.WriteString(fmt.Sprintf("**Expected Hash:** `%s`\n\n", originalHash.Hash))
				}
			}
		}
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: hoverText.String(),
		},
	}, nil
}

func (p *TwigVersioningHoverProvider) findBlockNameAfterComment(commentNode *tree_sitter.Node, content string) string {
	parent := commentNode.Parent()
	if parent == nil {
		return ""
	}

	for i := 0; i < int(parent.NamedChildCount()); i++ {
		child := parent.NamedChild(uint(i))
		if child == commentNode {
			for j := i + 1; j < int(parent.NamedChildCount()); j++ {
				nextChild := parent.NamedChild(uint(j))
				if nextChild.Kind() == "block" {
					for k := 0; k < int(nextChild.NamedChildCount()); k++ {
						blockChild := nextChild.NamedChild(uint(k))
						if blockChild.Kind() == "identifier" {
							return string(blockChild.Utf8Text([]byte(content)))
						}
					}
				}
			}
			break
		}
	}

	return ""
}
