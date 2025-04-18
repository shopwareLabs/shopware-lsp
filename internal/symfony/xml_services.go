package symfony

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	treesitterhelper "github.com/shopware/shopware-lsp/internal/tree_sitter_helper"
	tree_sitter_xml "github.com/tree-sitter-grammars/tree-sitter-xml/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Service represents a Symfony service definition
type Service struct {
	ID    string            // Service ID
	Class string            // Service class
	Tags  map[string]string // Service tags
	Path  string            // Source file path
	Line  int               // Line number in source file
}

// ServiceAlias represents a Symfony service alias
type ServiceAlias struct {
	ID     string // Alias ID
	Target string // Target service ID
	Path   string // Source file path
	Line   int    // Line number in source file
}

// Parameter represents a Symfony container parameter
type Parameter struct {
	Name  string // Parameter name
	Value string // Parameter value
	Path  string // Source file path
	Line  int    // Line number in source file
}

// xmlParserPool is a pool of reusable XML parsers
var xmlParserPool = sync.Pool{
	New: func() interface{} {
		parser := tree_sitter.NewParser()
		_ = parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_xml.LanguageXML()))
		return parser
	},
}

// ParseXMLServices parses Symfony XML service definitions and returns a list of services, aliases, and parameters.
// It can accept either a file path or direct content with a path.
func ParseXMLServices(pathOrContent interface{}, optionalPath ...string) ([]Service, []ServiceAlias, []Parameter, error) {
	var data []byte
	var path string

	// Determine if we're given a file path or content
	switch v := pathOrContent.(type) {
	case string:
		// We were given a file path
		path = v

		// Quick check of file before reading - if not a Symfony service config file, skip it
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, nil, err
		}

		// Read first 1024 bytes to quickly check for Symfony XML patterns
		peek := make([]byte, 1024)
		n, _ := f.Read(peek)

		if err := f.Close(); err != nil {
			return nil, nil, nil, err
		}

		if n > 0 {
			content := string(peek[:n])
			if !strings.Contains(content, "<container") &&
				!strings.Contains(content, "<services") &&
				!strings.Contains(content, "<service") {
				// Return empty results instead of error for non-service XML files
				// This maintains compatibility with previous behavior for tests
				return []Service{}, []ServiceAlias{}, []Parameter{}, nil
			}
		}

		// Now read the full file
		data, err = os.ReadFile(path)
		if err != nil {
			return nil, nil, nil, err
		}

	case []byte:
		// We were given content directly
		data = v

		// Use optional path if provided
		if len(optionalPath) > 0 {
			path = optionalPath[0]
		}

		// Quick check if this looks like a Symfony service file
		if len(data) > 0 {
			// Check a portion of the content for service patterns
			checkSize := len(data)
			if checkSize > 1024 {
				checkSize = 1024
			}

			content := string(data[:checkSize])
			if !strings.Contains(content, "<container") &&
				!strings.Contains(content, "<services") &&
				!strings.Contains(content, "<service") {
				// Return empty results instead of error for non-service XML files
				return []Service{}, []ServiceAlias{}, []Parameter{}, nil
			}
		}
	default:
		return nil, nil, nil, fmt.Errorf("expected string path or []byte content, got %T", pathOrContent)
	}

	// Get parser from pool and return it when done
	parserInterface := xmlParserPool.Get()
	parser, ok := parserInterface.(*tree_sitter.Parser)
	if !ok || parser == nil {
		parser = tree_sitter.NewParser()
		if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_xml.LanguageXML())); err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set XML language: %w", err)
		}
	}
	defer xmlParserPool.Put(parser)

	// Parse the XML content
	tree := parser.Parse(data, nil)
	if tree == nil {
		return nil, nil, nil, errors.New("failed to parse XML")
	}
	defer tree.Close()

	rootNode := tree.RootNode()
	if rootNode == nil {
		return nil, nil, nil, errors.New("failed to get root node")
	}

	// Pre-allocate with reasonable capacity
	services := make([]Service, 0, 50)
	aliases := make([]ServiceAlias, 0, 20)
	parameters := make([]Parameter, 0, 20)

	// Process container node
	containerNode := findContainerNode(rootNode, data)
	if containerNode == nil {
		return []Service{}, []ServiceAlias{}, []Parameter{}, nil
	}

	// Process content node directly for better handling of different XML structures
	if containerNode.NamedChildCount() > 1 {
		contentNode := containerNode.NamedChild(1)
		if contentNode != nil && contentNode.Kind() == "content" {
			// Process all elements in content
			childCount := int(contentNode.NamedChildCount())
			for i := 0; i < childCount; i++ {
				child := contentNode.NamedChild(uint(i))
				if child.Kind() == "element" {
					// Get element's STag or EmptyElemTag
					elementTag := child.NamedChild(0)
					if elementTag == nil {
						continue
					}

					// Get element name
					nameNode := treesitterhelper.GetFirstNodeOfKind(elementTag, "Name")
					if nameNode == nil {
						continue
					}

					elementName := nameNode.Utf8Text(data)
					switch string(elementName) {
					case "service":
						service := processServiceNode(child, data, path, nil)
						if service.ID != "" {
							services = append(services, service)
						}
					case "alias":
						alias := processAliasNode(child, data, path, nil)
						if alias.ID != "" && alias.Target != "" {
							aliases = append(aliases, alias)
						}
					case "services":
						// Process services inside the services tag
						nestedServices := processServicesNode(child, data, path, nil)
						services = append(services, nestedServices...)
					case "parameters":
						// Process parameters inside the parameters tag
						nestedParams := processParametersNode(child, data, path, nil)
						parameters = append(parameters, nestedParams...)
					case "parameter":
						param := processParameterNode(child, data, path, nil)
						if param.Name != "" {
							parameters = append(parameters, param)
						}
					}
				}
			}

			return services, aliases, parameters, nil
		}
	}

	// Fall back to the original approach if needed
	for i := 0; i < int(containerNode.NamedChildCount()); i++ {
		child := containerNode.NamedChild(uint(i))

		// Skip non-element nodes
		if child.Kind() != "element" {
			continue
		}

		// Get the element name - element has either STag or EmptyElemTag
		elementStartTag := child.NamedChild(0)
		if elementStartTag == nil {
			continue
		}

		nameNode := treesitterhelper.GetFirstNodeOfKind(elementStartTag, "Name")
		if nameNode == nil {
			continue
		}

		elementName := nameNode.Utf8Text(data)

		switch string(elementName) {
		case "service":
			service := processServiceNode(child, data, path, nil)
			if service.ID != "" {
				services = append(services, service)
			}
		case "alias":
			alias := processAliasNode(child, data, path, nil)
			if alias.ID != "" && alias.Target != "" {
				aliases = append(aliases, alias)
			}
		case "services":
			// Process services inside the services tag
			nestedServices := processServicesNode(child, data, path, nil)
			services = append(services, nestedServices...)
		case "parameters":
			// Process parameters inside the parameters tag
			nestedParams := processParametersNode(child, data, path, nil)
			parameters = append(parameters, nestedParams...)
		case "parameter":
			param := processParameterNode(child, data, path, nil)
			if param.Name != "" {
				parameters = append(parameters, param)
			}
		}
	}

	return services, aliases, parameters, nil
}

// findContainerNode finds the container node in the XML tree
func findContainerNode(rootNode *tree_sitter.Node, data []byte) *tree_sitter.Node {
	// For Symfony XML files, the container is usually the document element
	for i := 0; i < int(rootNode.NamedChildCount()); i++ {
		child := rootNode.NamedChild(uint(i))
		if child.Kind() == "element" {
			elementStartTag := child.NamedChild(0)
			if elementStartTag == nil {
				continue
			}
			nameNode := treesitterhelper.GetFirstNodeOfKind(elementStartTag, "Name")
			if nameNode != nil && string(nameNode.Utf8Text(data)) == "container" {
				return child
			}
		}
	}
	return nil
}

// processServiceNode extracts service information from a service element node
func processServiceNode(node *tree_sitter.Node, data []byte, path string, contentLines []string) Service {
	service := Service{
		Tags: make(map[string]string, 5), // Pre-allocate with typical capacity
		Path: path,
	}

	// Get start tag node (either STag or EmptyElemTag)
	startTag := node.NamedChild(0)
	if startTag == nil {
		return service
	}

	// Get attributes
	attrs := getXmlAttributeValues(startTag, data)
	service.ID = attrs["id"]

	if strings.Contains(service.ID, " ") {
		return Service{}
	}

	service.Class = attrs["class"]

	// If service has no class, use ID as class (Symfony default behavior)
	if service.Class == "" {
		service.Class = service.ID
	}

	// Skip processing if no ID (required for a valid service)
	if service.ID == "" {
		return service
	}

	// Fast line number calculation - just count newlines in the byte range
	startByte := int(node.StartByte())
	lineNum := 1 + bytes.Count(data[:startByte], []byte{'\n'})
	service.Line = lineNum

	// Only process tags if this isn't an empty element (has content)
	if startTag.Kind() == "STag" && node.NamedChildCount() > 2 {
		// Get content node (index 1 if we have STag, content, ETag)
		contentNode := node.NamedChild(1)
		if contentNode != nil && contentNode.Kind() == "content" {
			// Process all elements in the content
			childCount := int(contentNode.NamedChildCount())
			for i := 0; i < childCount; i++ {
				child := contentNode.NamedChild(uint(i))
				if child.Kind() != "element" {
					continue
				}

				// Get tag's STag or EmptyElemTag
				tagElement := child.NamedChild(0)
				if tagElement == nil {
					continue
				}

				// Get tag name
				tagNameNode := treesitterhelper.GetFirstNodeOfKind(tagElement, "Name")
				if tagNameNode == nil {
					continue
				}

				// Fast string comparison
				tagElementName := tagNameNode.Utf8Text(data)
				if string(tagElementName) == "tag" {
					// Get attributes on tag
					tagAttrs := getXmlAttributeValues(tagElement, data)
					if tagName := tagAttrs["name"]; tagName != "" {
						service.Tags[tagName] = ""
					}
				}
			}
		}
	}

	return service
}

// processAliasNode extracts alias information from an alias element node
func processAliasNode(node *tree_sitter.Node, data []byte, path string, contentLines []string) ServiceAlias {
	alias := ServiceAlias{
		Path: path,
	}

	// Get start tag node
	startTag := node.NamedChild(0)
	if startTag == nil {
		return alias
	}

	// Get attributes
	attrs := getXmlAttributeValues(startTag, data)
	alias.ID = attrs["id"]
	alias.Target = attrs["service"]

	// Skip if missing required attributes
	if alias.ID == "" || alias.Target == "" {
		return alias
	}

	// Fast line number calculation
	startByte := int(node.StartByte())
	lineNum := 1 + bytes.Count(data[:startByte], []byte{'\n'})
	alias.Line = lineNum

	return alias
}

// getXmlAttributeValues extracts XML attributes as a map
func getXmlAttributeValues(node *tree_sitter.Node, data []byte) map[string]string {
	return treesitterhelper.GetXmlAttributeValues(node, string(data))
}

// processServicesNode processes services inside a services element
func processServicesNode(node *tree_sitter.Node, data []byte, path string, contentLines []string) []Service {
	// Pre-allocate with reasonable capacity
	services := make([]Service, 0, 20)

	// Get the content node
	if node.NamedChildCount() < 2 {
		return services
	}

	contentNode := node.NamedChild(1)
	if contentNode == nil || contentNode.Kind() != "content" {
		return services
	}

	// Process all elements in content
	childCount := int(contentNode.NamedChildCount())
	for i := 0; i < childCount; i++ {
		child := contentNode.NamedChild(uint(i))
		if child.Kind() != "element" {
			continue
		}

		// Get element's STag or EmptyElemTag
		elementTag := child.NamedChild(0)
		if elementTag == nil {
			continue
		}

		// Get element name
		nameNode := treesitterhelper.GetFirstNodeOfKind(elementTag, "Name")
		if nameNode == nil {
			continue
		}

		// Fast string comparison
		elementName := nameNode.Utf8Text(data)
		if string(elementName) == "service" {
			service := processServiceNode(child, data, path, nil)
			if service.ID != "" {
				services = append(services, service)
			}
		}
	}

	return services
}

// processParameterNode extracts parameter information from a parameter element node
func processParameterNode(node *tree_sitter.Node, data []byte, path string, contentLines []string) Parameter {
	param := Parameter{
		Path: path,
	}

	// Get start tag node (either STag or EmptyElemTag)
	startTag := node.NamedChild(0)
	if startTag == nil {
		return param
	}

	// Get attributes
	attrs := getXmlAttributeValues(startTag, data)
	param.Name = attrs["key"] // In Symfony XML, parameters use "key" as attribute

	// Skip if missing required name attribute
	if param.Name == "" {
		return param
	}

	// Handle different types of parameter content
	// If it's an empty element tag with 'type' and 'id' attributes, it's a service reference
	if paramType, hasType := attrs["type"]; hasType && paramType == "service" {
		if serviceId, hasServiceId := attrs["id"]; hasServiceId {
			param.Value = "@" + serviceId // Symfony convention for service references
		}
	} else if value, hasValue := attrs["value"]; hasValue {
		// Simple value attribute
		param.Value = value
	} else if startTag.Kind() == "STag" && node.NamedChildCount() > 1 {
		// Parameter has content
		contentNode := node.NamedChild(1)
		if contentNode != nil && contentNode.Kind() == "content" {
			// Extract text value from content
			param.Value = strings.TrimSpace(string(contentNode.Utf8Text(data)))
		}
	}

	// Fast line number calculation
	startByte := int(node.StartByte())
	param.Line = 1 + bytes.Count(data[:startByte], []byte{'\n'})

	return param
}

// processParametersNode processes parameters inside a parameters element
func processParametersNode(node *tree_sitter.Node, data []byte, path string, contentLines []string) []Parameter {
	// Pre-allocate with reasonable capacity
	parameters := make([]Parameter, 0, 10)

	// Get the content node
	if node.NamedChildCount() < 2 {
		return parameters
	}

	contentNode := node.NamedChild(1)
	if contentNode == nil || contentNode.Kind() != "content" {
		return parameters
	}

	// Process all elements in content
	childCount := int(contentNode.NamedChildCount())
	for i := 0; i < childCount; i++ {
		child := contentNode.NamedChild(uint(i))
		if child.Kind() != "element" {
			continue
		}

		// Get element's STag or EmptyElemTag
		elementTag := child.NamedChild(0)
		if elementTag == nil {
			continue
		}

		// Get element name
		nameNode := treesitterhelper.GetFirstNodeOfKind(elementTag, "Name")
		if nameNode == nil {
			continue
		}

		// Fast string comparison
		elementName := nameNode.Utf8Text(data)
		if string(elementName) == "parameter" {
			param := processParameterNode(child, data, path, nil)
			if param.Name != "" {
				parameters = append(parameters, param)
			}
		}
	}

	return parameters
}

// GetServiceIDs extracts just the service IDs from a list of services
func GetServiceIDs(services []Service, aliases []ServiceAlias) []string {
	result := make([]string, 0, len(services)+len(aliases))

	// Add service IDs
	for _, service := range services {
		result = append(result, service.ID)
	}

	// Add alias IDs
	for _, alias := range aliases {
		result = append(result, alias.ID)
	}

	return result
}

// GetParameterNames extracts just the parameter names from a list of parameters
func GetParameterNames(parameters []Parameter) []string {
	result := make([]string, 0, len(parameters))

	for _, param := range parameters {
		result = append(result, param.Name)
	}

	return result
}
