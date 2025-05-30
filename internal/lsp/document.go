package lsp

import (
	"path/filepath"
	"strings"
	"sync"

	"github.com/shopware/shopware-lsp/internal/indexer"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// TextDocument represents a document open in the editor
type TextDocument struct {
	URI     string
	Text    []byte
	Version int
	Tree    *tree_sitter.Tree
}

// DocumentManager manages text documents
type DocumentManager struct {
	documents map[string]*TextDocument
	mu        sync.RWMutex
	parsers   map[string]*tree_sitter.Parser
}

// NewDocumentManager creates a new document manager
func NewDocumentManager() *DocumentManager {
	return &DocumentManager{
		documents: make(map[string]*TextDocument),
		parsers:   indexer.CreateTreesitterParsers(),
	}
}

// OpenDocument adds or updates a document
func (m *DocumentManager) OpenDocument(uri string, text string, version int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	doc := &TextDocument{
		URI:     uri,
		Text:    []byte(text),
		Version: version,
	}

	fileType := strings.ToLower(filepath.Ext(uri))

	if parser, ok := m.parsers[fileType]; ok {
		doc.Tree = parser.Parse(doc.Text, nil)
	}

	m.documents[uri] = doc
}

// UpdateDocument updates an existing document
func (m *DocumentManager) UpdateDocument(uri string, text string, version int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if doc, ok := m.documents[uri]; ok {
		doc.Text = []byte(text)
		doc.Version = version

		fileType := strings.ToLower(filepath.Ext(uri))

		if parser, ok := m.parsers[fileType]; ok {
			doc.Tree = parser.Parse(doc.Text, nil)
		}
	} else {
		// If the document doesn't exist, create it
		doc := &TextDocument{
			URI:     uri,
			Text:    []byte(text),
			Version: version,
		}

		fileType := strings.ToLower(filepath.Ext(uri))

		if parser, ok := m.parsers[fileType]; ok {
			doc.Tree = parser.Parse(doc.Text, nil)
		}

		m.documents[uri] = doc
	}
}

// CloseDocument removes a document
func (m *DocumentManager) CloseDocument(uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close the tree if it exists
	if doc, ok := m.documents[uri]; ok && doc.Tree != nil {
		doc.Tree.Close()
	}

	delete(m.documents, uri)
}

// GetDocument returns a document by URI
func (m *DocumentManager) GetDocument(uri string) (*TextDocument, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	doc, ok := m.documents[uri]
	return doc, ok
}

// GetDocumentText returns the text of a document by URI
func (m *DocumentManager) GetDocumentText(uri string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if doc, ok := m.documents[uri]; ok {
		return doc.Text, true
	}
	return nil, false
}

func (m *DocumentManager) GetNodeAtPosition(uri string, line int, character int) (*tree_sitter.Node, *TextDocument, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if the document exists
	doc, ok := m.documents[uri]
	if !ok || doc.Tree == nil {
		return nil, nil, false
	}

	// Find the closest element to our cursor position
	treeSitterPos := tree_sitter.Point{
		Row:    uint(line),
		Column: uint(character),
	}

	// Manual tree traversal to find the most specific node at position
	node := m.findNodeAtPosition(doc.Tree.RootNode(), treeSitterPos, doc.Text)
	if node != nil {
		return node, doc, true
	}

	// Fallback to standard method
	node = doc.Tree.RootNode().NamedDescendantForPointRange(treeSitterPos, treeSitterPos)
	return node, doc, true
}

func (m *DocumentManager) findNodeAtPosition(node *tree_sitter.Node, pos tree_sitter.Point, text []byte) *tree_sitter.Node {
	if node == nil {
		return nil
	}

	// Convert position to byte offset
	lines := string(text)
	targetOffset := 0
	for i, line := range strings.Split(lines, "\n") {
		if uint(i) == pos.Row {
			targetOffset += int(pos.Column)
			break
		}
		if uint(i) < pos.Row {
			targetOffset += len(line) + 1 // +1 for newline
		}
	}

	// Check if this node contains the target position
	if node.StartByte() <= uint(targetOffset) && uint(targetOffset) <= node.EndByte() {
		// Check all children (including unnamed ones)
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child != nil {
				childResult := m.findNodeAtPosition(child, pos, text)
				if childResult != nil {
					return childResult
				}
			}
		}
		// If no child contains it, this node is the most specific
		return node
	}

	return nil
}

func (m *DocumentManager) GetRootNode(uri string) *tree_sitter.Node {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if the document exists
	doc, ok := m.documents[uri]
	if !ok || doc.Tree == nil {
		return nil
	}

	return doc.Tree.RootNode()
}

// Close closes the document manager and frees resources
func (m *DocumentManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Close all trees
	for _, doc := range m.documents {
		if doc.Tree != nil {
			doc.Tree.Close()
			doc.Tree = nil
		}
	}

	indexer.CloseTreesitterParsers(m.parsers)
}
