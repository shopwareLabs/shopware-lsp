package indexer

import (
	tree_sitter_twig "github.com/shopware/shopware-lsp/internal/tree_sitter_grammars/twig/bindings/go"
	tree_sitter_scss "github.com/tree-sitter-grammars/tree-sitter-scss/bindings/go"
	tree_sitter_xml "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go"
	tree_sitter_yaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_json "github.com/tree-sitter/tree-sitter-json/bindings/go"
	tree_sitter_php "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

var scannedFileTypes = []string{
	".php",
	".xml",
	".yaml",
	".yml",
	".twig",
	".json",
	".scss",
}

func CreateTreesitterParsers() map[string]*tree_sitter.Parser {
	parsers := make(map[string]*tree_sitter.Parser)

	parsers[".php"] = tree_sitter.NewParser()
	if err := parsers[".php"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_php.LanguagePHP())); err != nil {
		panic(err)
	}

	parsers[".xml"] = tree_sitter.NewParser()
	if err := parsers[".xml"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_xml.LanguageXML())); err != nil {
		panic(err)
	}

	parsers[".twig"] = tree_sitter.NewParser()
	if err := parsers[".twig"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_twig.Language())); err != nil {
		panic(err)
	}

	parsers[".yaml"] = tree_sitter.NewParser()
	if err := parsers[".yaml"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_yaml.Language())); err != nil {
		panic(err)
	}

	parsers[".yml"] = tree_sitter.NewParser()
	if err := parsers[".yml"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_yaml.Language())); err != nil {
		panic(err)
	}

	parsers[".scss"] = tree_sitter.NewParser()
	if err := parsers[".scss"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_scss.Language())); err != nil {
		panic(err)
	}

	parsers[".json"] = tree_sitter.NewParser()
	if err := parsers[".json"].SetLanguage(tree_sitter.NewLanguage(tree_sitter_json.Language())); err != nil {
		panic(err)
	}

	return parsers
}

func CloseTreesitterParsers(parsers map[string]*tree_sitter.Parser) {
	for _, parser := range parsers {
		parser.Close()
	}
}
