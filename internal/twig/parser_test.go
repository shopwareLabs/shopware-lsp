package twig

import (
	"testing"

	tree_sitter_twig "github.com/shopware/shopware-lsp/internal/tree_sitter_grammars/twig/bindings/go"
	"github.com/stretchr/testify/assert"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestTwigParse(t *testing.T) {
	parser := tree_sitter.NewParser()

	assert.NoError(t, parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_twig.Language())))

	content := []byte(`{% block foo %}{% endblock %}`)

	tree := parser.Parse(content, nil)
	defer tree.Close()

	file, err := ParseTwig("test", tree.RootNode(), content)
	assert.NoError(t, err)

	assert.Equal(t, "test", file.Path)
	
	block, exists := file.Blocks["foo"]
	assert.True(t, exists)
	assert.Equal(t, "foo", block.Name)
	assert.Equal(t, 1, block.Line)
	assert.NotEmpty(t, block.Hash)
	assert.Equal(t, "{% block foo %}{% endblock %}", block.Text)
}

func TestTwigParseSwExtends(t *testing.T) {
	parser := tree_sitter.NewParser()

	assert.NoError(t, parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_twig.Language())))

	content := []byte(`{% sw_extends '@Storefront/storefront/base.html.twig' %}`)

	tree := parser.Parse(content, nil)
	defer tree.Close()

	file, err := ParseTwig("test", tree.RootNode(), content)
	assert.NoError(t, err)

	assert.Equal(t, "test", file.Path)
	assert.Equal(t, "@Storefront/storefront/base.html.twig", file.ExtendsFile)
}

func TestNestedBlock(t *testing.T) {
	tpl := `
{% block a %}
	{% block b %}
		{% block c %}
		{% endblock %}
	{% endblock %}
{% endblock %}
`

	parser := tree_sitter.NewParser()

	assert.NoError(t, parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_twig.Language())))

	tree := parser.Parse([]byte(tpl), nil)
	defer tree.Close()

	file, err := ParseTwig("test", tree.RootNode(), []byte(tpl))
	assert.NoError(t, err)

	assert.Equal(t, "test", file.Path)
	assert.Len(t, file.Blocks, 3)
	
	blockA, existsA := file.Blocks["a"]
	assert.True(t, existsA)
	assert.Equal(t, "a", blockA.Name)
	assert.Equal(t, 2, blockA.Line)
	assert.NotEmpty(t, blockA.Hash)
	
	blockB, existsB := file.Blocks["b"]
	assert.True(t, existsB)
	assert.Equal(t, "b", blockB.Name)
	assert.Equal(t, 3, blockB.Line)
	
	blockC, existsC := file.Blocks["c"]
	assert.True(t, existsC)
	assert.Equal(t, "c", blockC.Name)
	assert.Equal(t, 4, blockC.Line)
}

func TestBlockWithVersionComment(t *testing.T) {
	tpl := `{% sw_extends '@Storefront/storefront/base.html.twig' %}

{# shopware-block: abc123def456@6.4.15.0 #}
{% block foo %}
    content
{% endblock %}
`

	parser := tree_sitter.NewParser()
	assert.NoError(t, parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_twig.Language())))

	tree := parser.Parse([]byte(tpl), nil)
	defer tree.Close()

	file, err := ParseTwig("test", tree.RootNode(), []byte(tpl))
	assert.NoError(t, err)

	block, exists := file.Blocks["foo"]
	assert.True(t, exists)
	assert.NotNil(t, block.VersionComment)
	assert.Equal(t, "abc123def456", block.VersionComment.Hash)
	assert.Equal(t, "6.4.15.0", block.VersionComment.Version)
	assert.Equal(t, 3, block.VersionComment.Line)
}
