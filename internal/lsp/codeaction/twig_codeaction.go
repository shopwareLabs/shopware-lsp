package codeaction

import (
	"context"
	"strings"

	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	treesitterhelper "github.com/shopware/shopware-lsp/internal/tree_sitter_helper"
	"github.com/shopware/shopware-lsp/internal/twig"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type TwigCodeActionProvider struct {
	twigIndexer *twig.TwigIndexer
	projectRoot string
}

func NewTwigCodeActionProvider(projectRoot string, server *lsp.Server) *TwigCodeActionProvider {
	indexer, ok := server.GetIndexer("twig.indexer")
	if !ok {
		return &TwigCodeActionProvider{twigIndexer: nil, projectRoot: projectRoot}
	}
	twigIndexer, ok := indexer.(*twig.TwigIndexer)
	if !ok {
		return &TwigCodeActionProvider{twigIndexer: nil, projectRoot: projectRoot}
	}
	return &TwigCodeActionProvider{twigIndexer: twigIndexer, projectRoot: projectRoot}
}

func (p *TwigCodeActionProvider) GetCodeActionKinds() []protocol.CodeActionKind {
	return []protocol.CodeActionKind{
		protocol.CodeActionRefactorExtract,
		protocol.CodeActionQuickFix,
	}
}

func (p *TwigCodeActionProvider) GetCodeActions(ctx context.Context, params *protocol.CodeActionParams) []protocol.CodeAction {
	if params.Node == nil {
		return nil
	}

	var codeActions []protocol.CodeAction

	if IsBlock().Matches(params.Node, params.DocumentContent) {
		if strings.Contains(params.TextDocument.URI, "Resources/views/storefront") {
			textValue := treesitterhelper.GetNodeText(params.Node, params.DocumentContent)

			codeActions = append(codeActions, protocol.CodeAction{
				Title: "Overwrite this block in Extension",
				Kind:  protocol.CodeActionRefactorExtract,
				Command: &protocol.CommandAction{
					Title:     "Overwrite Block",
					Command:   "shopware.twig.extendBlock",
					Arguments: []any{params.TextDocument.URI, textValue},
				},
			})
		}

		if action := p.getVersioningHashAction(params); action != nil {
			codeActions = append(codeActions, *action)
		}

		if action := p.getShowDiffAction(params); action != nil {
			codeActions = append(codeActions, *action)
		}
	}

	if action := p.getShowDiffActionFromComment(params); action != nil {
		codeActions = append(codeActions, *action)
	}

	return codeActions
}

func (p *TwigCodeActionProvider) getVersioningHashAction(params *protocol.CodeActionParams) *protocol.CodeAction {
	if p.twigIndexer == nil {
		return nil
	}

	if twig.IsStorefrontTemplate(params.TextDocument.URI) {
		return nil
	}

	blockNode := params.Node.Parent()
	if blockNode == nil || blockNode.Kind() != "block" {
		return nil
	}

	blockName := treesitterhelper.GetNodeText(params.Node, params.DocumentContent)

	if p.hasVersioningComment(blockNode, params.DocumentContent) {
		return nil
	}

	allBlockHashes, err := p.twigIndexer.GetTwigBlockHashes(blockName)
	if err != nil || len(allBlockHashes) == 0 {
		return nil
	}

	originalHash := twig.FindOriginalStorefrontHash(allBlockHashes)
	if originalHash == nil {
		return nil
	}

	blockLine := int(blockNode.Range().StartPoint.Row)
	versionComment := twig.FormatVersionComment(originalHash.Hash, twig.DetectShopwareVersion(p.projectRoot))

	edit := &protocol.WorkspaceEdit{
		Changes: map[string][]protocol.TextEdit{
			params.TextDocument.URI: {
				{
					Range: protocol.Range{
						Start: protocol.Position{Line: blockLine, Character: 0},
						End:   protocol.Position{Line: blockLine, Character: 0},
					},
					NewText: versionComment,
				},
			},
		},
	}

	return &protocol.CodeAction{
		Title: "Add twig versioning hash",
		Kind:  protocol.CodeActionQuickFix,
		Edit:  edit,
	}
}

func (p *TwigCodeActionProvider) getShowDiffAction(params *protocol.CodeActionParams) *protocol.CodeAction {
	if p.twigIndexer == nil {
		return nil
	}

	if twig.IsStorefrontTemplate(params.TextDocument.URI) {
		return nil
	}

	blockNode := params.Node.Parent()
	if blockNode == nil || blockNode.Kind() != "block" {
		return nil
	}

	blockName := treesitterhelper.GetNodeText(params.Node, params.DocumentContent)

	rootNode := params.Node
	for rootNode.Parent() != nil {
		rootNode = rootNode.Parent()
	}

	twigFile, err := twig.ParseTwig(params.TextDocument.URI, rootNode, params.DocumentContent)
	if err != nil {
		return nil
	}

	block, exists := twigFile.Blocks[blockName]
	if !exists || block.VersionComment == nil {
		return nil
	}

	allBlockHashes, err := p.twigIndexer.GetTwigBlockHashes(blockName)
	if err != nil || len(allBlockHashes) == 0 {
		return nil
	}

	originalHash := twig.FindOriginalStorefrontHash(allBlockHashes)
	if originalHash == nil {
		return nil
	}

	if block.VersionComment.Hash == originalHash.Hash {
		return nil
	}

	return &protocol.CodeAction{
		Title: "Show block difference",
		Kind:  protocol.CodeActionQuickFix,
		Command: &protocol.CommandAction{
			Title:     "Show Block Difference",
			Command:   "shopware.twig.showBlockDiff",
			Arguments: []any{params.TextDocument.URI, blockName},
		},
	}
}

func (p *TwigCodeActionProvider) getShowDiffActionFromComment(params *protocol.CodeActionParams) *protocol.CodeAction {
	if p.twigIndexer == nil {
		return nil
	}

	if twig.IsStorefrontTemplate(params.TextDocument.URI) {
		return nil
	}

	if params.Node.Kind() != "comment" {
		return nil
	}

	commentText := string(params.Node.Utf8Text(params.DocumentContent))
	if !strings.Contains(commentText, twig.VersionCommentPrefix) {
		return nil
	}

	versionComment := twig.ParseVersionComment(commentText, int(params.Node.Range().StartPoint.Row)+1)
	if versionComment == nil {
		return nil
	}

	commentLine := int(params.Node.Range().StartPoint.Row) + 1

	rootNode := params.Node
	for rootNode.Parent() != nil {
		rootNode = rootNode.Parent()
	}

	twigFile, err := twig.ParseTwig(params.TextDocument.URI, rootNode, params.DocumentContent)
	if err != nil {
		return nil
	}

	var blockName string
	for _, block := range twigFile.Blocks {
		if block.VersionComment != nil && block.VersionComment.Line == commentLine {
			blockName = block.Name
			break
		}
	}

	if blockName == "" {
		return nil
	}

	allBlockHashes, err := p.twigIndexer.GetTwigBlockHashes(blockName)
	if err != nil || len(allBlockHashes) == 0 {
		return nil
	}

	originalHash := twig.FindOriginalStorefrontHash(allBlockHashes)
	if originalHash == nil {
		return nil
	}

	if versionComment.Hash == originalHash.Hash {
		return nil
	}

	return &protocol.CodeAction{
		Title: "Show block difference",
		Kind:  protocol.CodeActionQuickFix,
		Command: &protocol.CommandAction{
			Title:     "Show Block Difference",
			Command:   "shopware.twig.showBlockDiff",
			Arguments: []any{params.TextDocument.URI, blockName},
		},
	}
}

func (p *TwigCodeActionProvider) hasVersioningComment(blockNode *tree_sitter.Node, content []byte) bool {
	parent := blockNode.Parent()
	if parent == nil {
		return false
	}

	blockStartLine := blockNode.Range().StartPoint.Row

	for i := 0; i < int(parent.NamedChildCount()); i++ {
		child := parent.NamedChild(uint(i))

		if child.Range().StartPoint.Row == blockNode.Range().StartPoint.Row &&
			child.Range().StartPoint.Column == blockNode.Range().StartPoint.Column {
			if i > 0 {
				prevSibling := parent.NamedChild(uint(i - 1))
				if prevSibling.Kind() == "comment" {
					commentEndLine := prevSibling.Range().EndPoint.Row
					if blockStartLine-commentEndLine <= 1 {
						commentText := string(prevSibling.Utf8Text(content))
						if strings.Contains(commentText, twig.VersionCommentPrefix) {
							return true
						}
					}
				}
			}
			break
		}
	}
	return false
}

func IsBlock() treesitterhelper.Pattern {
	return treesitterhelper.And(
		treesitterhelper.NodeKind("identifier"),
		treesitterhelper.Ancestor(
			treesitterhelper.NodeKind("block"),
			1,
		),
	)
}
