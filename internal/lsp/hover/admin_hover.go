package hover

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shopware/shopware-lsp/internal/admin"
	"github.com/shopware/shopware-lsp/internal/lsp"
	"github.com/shopware/shopware-lsp/internal/lsp/protocol"
	treesitterhelper "github.com/shopware/shopware-lsp/internal/tree_sitter_helper"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// AdminHoverProvider provides hover information for Shopware Admin Vue components
type AdminHoverProvider struct {
	adminIndexer *admin.AdminComponentIndexer
	projectRoot  string
}

// NewAdminHoverProvider creates a new admin hover provider
func NewAdminHoverProvider(projectRoot string, lspServer *lsp.Server) *AdminHoverProvider {
	adminIndexer, _ := lspServer.GetIndexer("admin.component.indexer")

	return &AdminHoverProvider{
		adminIndexer: adminIndexer.(*admin.AdminComponentIndexer),
		projectRoot:  projectRoot,
	}
}

// GetHover returns hover information for Vue components
func (p *AdminHoverProvider) GetHover(ctx context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	if params.Node == nil {
		return nil, nil
	}

	ext := strings.ToLower(filepath.Ext(params.TextDocument.URI))

	// Handle JS/TS files
	if ext == ".js" || ext == ".ts" {
		return p.jsHover(ctx, params)
	}

	// Handle Twig files (admin templates)
	if ext == ".twig" {
		// Only process Twig files in administration directory
		if strings.Contains(params.TextDocument.URI, "Resources/app/administration") {
			return p.twigHover(ctx, params)
		}
	}

	return nil, nil
}

// twigHover handles hover for Vue components in Twig templates
func (p *AdminHoverProvider) twigHover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	node := params.Node
	content := params.DocumentContent

	// Check if we're on an HTML tag name
	if node.Kind() != "html_tag_name" {
		return nil, nil
	}

	componentName := string(node.Utf8Text(content))
	if componentName == "" {
		return nil, nil
	}

	// Look up the component with its definition
	components, err := p.adminIndexer.GetComponentWithDefinition(componentName)
	if err != nil || len(components) == 0 {
		return nil, nil
	}

	// Build markdown content for the hover
	markdown := p.buildHoverContent(components)

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: markdown,
		},
		Range: &protocol.Range{
			Start: protocol.Position{
				Line:      int(node.StartPosition().Row),
				Character: int(node.StartPosition().Column),
			},
			End: protocol.Position{
				Line:      int(node.EndPosition().Row),
				Character: int(node.EndPosition().Column),
			},
		},
	}, nil
}

func (p *AdminHoverProvider) jsHover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	node := params.Node
	content := params.DocumentContent

	// Check if we're on a string that could be a component name
	if node.Kind() != "string" && node.Kind() != "string_fragment" {
		return nil, nil
	}

	// Check if this string is in a Component.extend or Component.register call
	if !p.isInComponentCall(node, content) {
		return nil, nil
	}

	// Extract the component name
	componentName := p.extractComponentName(node, content)
	if componentName == "" {
		return nil, nil
	}

	// Look up the component with its definition
	components, err := p.adminIndexer.GetComponentWithDefinition(componentName)
	if err != nil || len(components) == 0 {
		return nil, nil
	}

	// Build markdown content for the hover
	markdown := p.buildHoverContent(components)

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: markdown,
		},
		Range: &protocol.Range{
			Start: protocol.Position{
				Line:      params.Position.Line,
				Character: params.Position.Character,
			},
			End: protocol.Position{
				Line:      params.Position.Line,
				Character: params.Position.Character + len(componentName),
			},
		},
	}, nil
}

// buildHoverContent creates the markdown content for the hover popup
func (p *AdminHoverProvider) buildHoverContent(components []admin.VueComponent) string {
	var sb strings.Builder

	for i, comp := range components {
		if i > 0 {
			sb.WriteString("\n---\n\n")
		}

		// Component name header
		fmt.Fprintf(&sb, "## `%s`\n\n", comp.Name)

		// Show if it extends another component
		if comp.ExtendsComponent != "" {
			fmt.Fprintf(&sb, "**Extends**: `%s`\n\n", comp.ExtendsComponent)
		}

		// Props section
		if len(comp.Props) > 0 {
			sb.WriteString("### Props\n\n")
			for _, prop := range comp.Props {
				propLine := fmt.Sprintf("- `%s`", prop.Name)
				if prop.Type != "" {
					propLine += fmt.Sprintf(": **%s**", prop.Type)
				}
				if prop.Required {
					propLine += " *(required)*"
				}
				if prop.Default != "" {
					propLine += fmt.Sprintf(" = `%s`", prop.Default)
				}
				sb.WriteString(propLine + "\n")
			}
			sb.WriteString("\n")
		}

		// Emits section
		if len(comp.Emits) > 0 {
			sb.WriteString("### Events\n\n")
			for _, emit := range comp.Emits {
				fmt.Fprintf(&sb, "- `%s`\n", emit)
			}
			sb.WriteString("\n")
		}

		// Methods section
		if len(comp.Methods) > 0 {
			sb.WriteString("### Methods\n\n")
			for _, method := range comp.Methods {
				fmt.Fprintf(&sb, "- `%s()`\n", method)
			}
			sb.WriteString("\n")
		}

		// Computed section
		if len(comp.Computed) > 0 {
			sb.WriteString("### Computed\n\n")
			for _, computed := range comp.Computed {
				fmt.Fprintf(&sb, "- `%s`\n", computed)
			}
			sb.WriteString("\n")
		}

		// Slots section
		if len(comp.Slots) > 0 {
			sb.WriteString("### Slots\n\n")
			for _, slot := range comp.Slots {
				fmt.Fprintf(&sb, "- `%s`\n", slot.Name)
			}
			sb.WriteString("\n")
		}

		// File path (relative to project root)
		if comp.DefinitionPath != "" {
			displayPath := p.makeRelativePath(comp.DefinitionPath)
			fmt.Fprintf(&sb, "*Defined in*: `%s`\n", displayPath)
		} else if comp.FilePath != "" {
			displayPath := p.makeRelativePath(comp.FilePath)
			fmt.Fprintf(&sb, "*Registered in*: `%s`\n", displayPath)
		}
	}

	return sb.String()
}

// makeRelativePath converts an absolute path to a path relative to the project root
func (p *AdminHoverProvider) makeRelativePath(absPath string) string {
	if p.projectRoot == "" {
		return absPath
	}
	relPath, err := filepath.Rel(p.projectRoot, absPath)
	if err != nil {
		return absPath
	}
	return relPath
}

// isInComponentCall checks if the node is within a Component.register/extend call
func (p *AdminHoverProvider) isInComponentCall(node *tree_sitter.Node, content []byte) bool {
	pattern := treesitterhelper.Ancestor(
		treesitterhelper.And(
			treesitterhelper.NodeKind("call_expression"),
			treesitterhelper.HasChild(
				treesitterhelper.And(
					treesitterhelper.NodeKind("member_expression"),
					treesitterhelper.Or(
						treesitterhelper.NodeText("Shopware.Component.register"),
						treesitterhelper.NodeText("Shopware.Component.extend"),
						treesitterhelper.NodeText("Component.register"),
						treesitterhelper.NodeText("Component.extend"),
					),
				),
			),
		),
		5,
	)

	return pattern.Matches(node, content)
}

// extractComponentName extracts the string content from the node
func (p *AdminHoverProvider) extractComponentName(node *tree_sitter.Node, content []byte) string {
	if node.Kind() == "string_fragment" {
		return string(node.Utf8Text(content))
	}

	if node.Kind() == "string" {
		// Find the string_fragment child
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.Kind() == "string_fragment" {
				return string(child.Utf8Text(content))
			}
		}
		// Fallback: trim quotes
		text := string(node.Utf8Text(content))
		return strings.Trim(text, "\"'")
	}

	return ""
}
