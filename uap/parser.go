package uap

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/ua-parser/uap-go/uaparser"
	"gopkg.in/yaml.v3"
)

//go:embed uap_regexps.yaml
var regexDefinitionsYAML []byte

// Parser resolves Android device details from the Google Play model catalog,
// then optionally falls back to uap-go when the catalog has no match.
type Parser struct {
	catalog *Catalog
	uap     *uaparser.Parser
}

var sharedParser = sync.OnceValue(func() *Parser {
	var definitions uaparser.RegexDefinitions
	if err := yaml.Unmarshal(regexDefinitionsYAML, &definitions); err != nil {
		panic(fmt.Errorf("parse embedded uap-go regex definitions: %w", err))
	}
	parser, err := uaparser.New(uaparser.WithRegexDefinitions(definitions), uaparser.WithSort(true))
	if err != nil {
		panic(err)
	}
	return &Parser{catalog: DefaultCatalog(), uap: parser}
})

// Default returns the shared read-only parser.
func Default() *Parser { return sharedParser() }

// ResolveDevice checks the Google Play device model catalog first and falls
// back to uap-go only when fallbackEnabled is true.
func (p *Parser) ResolveDevice(raw string, fallbackEnabled bool) Device {
	if p == nil || p.catalog == nil {
		return Device{}
	}
	return resolveDevice(p.catalog, raw, p.uap, fallbackEnabled)
}

// ResolveDeviceWith applies the same resolution sequence with a caller-owned
// uap-go parser, so applications can share their existing parser instance.
func ResolveDeviceWith(raw string, parser *uaparser.Parser, fallbackEnabled bool) Device {
	return resolveDevice(DefaultCatalog(), raw, parser, fallbackEnabled)
}

func resolveDevice(catalog *Catalog, raw string, parser *uaparser.Parser, fallbackEnabled bool) Device {
	if catalog == nil {
		return Device{}
	}
	token := AndroidModel(raw)
	if token == "K" {
		return Device{}
	}
	if device, ok := catalog.LookupModel(token, ""); ok {
		return device
	}
	if !fallbackEnabled || parser == nil {
		return Device{}
	}
	parsed := parser.ParseDevice(raw)
	brand, model := NormalizeBrand(parsed.Brand), strings.TrimSpace(parsed.Model)
	if device, ok := catalog.LookupModel(token, brand); ok {
		return device
	}
	if device, ok := catalog.LookupModel(model, brand); ok {
		return device
	}
	if brand == "" && strings.Contains(raw, "MIUI") {
		brand = "Xiaomi"
	}
	if UnknownModel(model) {
		model = ""
	}
	return Device{Brand: brand, Model: model}
}
