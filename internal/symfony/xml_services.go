package symfony

import (
	"bytes"
	"strings"

	treesitterhelper "github.com/shopware/shopware-lsp/internal/tree_sitter_helper"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Service represents a Symfony service definition
type Service struct {
	ID          string            // Service ID
	Class       string            // Service class
	AliasTarget string            // Service alias target
	Tags        map[string]string // Service tags
	Path        string            // Source file path
	Line        int               // Line number in source file
}

// Parameter represents a Symfony container parameter
type Parameter struct {
	Name  string // Parameter name
	Value string // Parameter value
	Path  string // Source file path
	Line  int    // Line number in source file
}

// ParseXMLServices parses Symfony XML service definitions and returns a list of services, aliases, and parameters.
// It can accept either a file path or direct content with a path.
func ParseXMLServices(path string, rootNode *tree_sitter.Node, data []byte) ([]Service, []Parameter, error) {
	// Pre-allocate with reasonable capacity
	services := make([]Service, 0, 50)
	parameters := make([]Parameter, 0, 20)

	// Process container node
	containerNode := findContainerNode(rootNode, data)
	if containerNode == nil {
		return []Service{}, []Parameter{}, nil
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
						service := processServiceNode(child, data, path)
						if service.ID != "" {
							services = append(services, service)
						}
					case "alias":
						alias := processAliasNode(child, data, path)
						if alias.ID != "" {
							services = append(services, alias)
						}
					case "services":
						// Process services inside the services tag
						nestedServices := processServicesNode(child, data, path)
						services = append(services, nestedServices...)
					case "parameters":
						// Process parameters inside the parameters tag
						nestedParams := processParametersNode(child, data, path)
						parameters = append(parameters, nestedParams...)
					case "parameter":
						param := processParameterNode(child, data, path)
						if param.Name != "" {
							parameters = append(parameters, param)
						}
					}
				}
			}

			return services, parameters, nil
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
			service := processServiceNode(child, data, path)
			if service.ID != "" {
				services = append(services, service)
			}
		case "alias":
			alias := processAliasNode(child, data, path)
			if alias.ID != "" && alias.AliasTarget != "" {
				services = append(services, alias)
			}
		case "services":
			// Process services inside the services tag
			nestedServices := processServicesNode(child, data, path)
			services = append(services, nestedServices...)
		case "parameters":
			// Process parameters inside the parameters tag
			nestedParams := processParametersNode(child, data, path)
			parameters = append(parameters, nestedParams...)
		case "parameter":
			param := processParameterNode(child, data, path)
			if param.Name != "" {
				parameters = append(parameters, param)
			}
		}
	}

	return services, parameters, nil
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
func processServiceNode(node *tree_sitter.Node, data []byte, path string) Service {
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
	attrs := treesitterhelper.GetXmlAttributeValues(startTag, data)
	service.ID = attrs["id"]

	// Skip processing if missing ID or ID contains spaces
	if service.ID == "" || strings.Contains(service.ID, " ") {
		return Service{}
	}

	service.Class = attrs["class"]

	// If service has no class, use ID as class (Symfony default behavior)
	if service.Class == "" {
		service.Class = service.ID
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
					tagAttrs := treesitterhelper.GetXmlAttributeValues(tagElement, data)
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
func processAliasNode(node *tree_sitter.Node, data []byte, path string) Service {
	alias := Service{
		Path: path,
	}

	// Get start tag node
	startTag := node.NamedChild(0)
	if startTag == nil {
		return alias
	}

	// Get attributes
	attrs := treesitterhelper.GetXmlAttributeValues(startTag, data)
	alias.ID = attrs["id"]
	alias.AliasTarget = attrs["service"]

	// Skip if missing required attributes
	if alias.ID == "" || alias.AliasTarget == "" {
		return Service{} // Return empty service if missing required attributes
	}

	// Fast line number calculation
	startByte := int(node.StartByte())
	lineNum := 1 + bytes.Count(data[:startByte], []byte{'\n'})
	alias.Line = lineNum

	return alias
}

// processServicesNode processes services inside a services element
func processServicesNode(node *tree_sitter.Node, data []byte, path string) []Service {
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
			service := processServiceNode(child, data, path)
			if service.ID != "" {
				services = append(services, service)
			}
		}
	}

	return services
}

// processParameterNode extracts parameter information from a parameter element node
func processParameterNode(node *tree_sitter.Node, data []byte, path string) Parameter {
	param := Parameter{
		Path: path,
	}

	// Get start tag node (either STag or EmptyElemTag)
	startTag := node.NamedChild(0)
	if startTag == nil {
		return param
	}

	// Get attributes
	attrs := treesitterhelper.GetXmlAttributeValues(startTag, data)
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
func processParametersNode(node *tree_sitter.Node, data []byte, path string) []Parameter {
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
			param := processParameterNode(child, data, path)
			if param.Name != "" {
				parameters = append(parameters, param)
			}
		}
	}

	return parameters
}
