package extension

import (
	"path/filepath"
	"strings"
)

type ShopwareExtensionType int

const (
	ShopwareExtensionTypeBundle ShopwareExtensionType = iota
	ShopwareExtensionTypeApp
)

type ShopwareExtension struct {
	Name string
	Type ShopwareExtensionType
	Path string
}

func (e ShopwareExtension) GetStorefrontViewsPath() string {
	path := strings.TrimSuffix(e.Path, string(filepath.Separator)+e.Name+".php")
	return filepath.Join(path, "Resources", "views")
}
