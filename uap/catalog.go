// Package deviceua provides a read-only device model index. It performs no
// network or file I/O at runtime; the catalog is versioned with the binary.
package uap

import (
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

//go:embed catalog.csv
var catalogCSV string

//go:embed catalog.meta.json
var catalogMetadata string

type Device struct{ Brand, Model string }

type modelEntry struct {
	device Device
	// Only ambiguous keys allocate alternatives. Returned devices are values;
	// no caller can mutate the shared index.
	alternatives []Device
}

type Catalog struct{ models map[string]modelEntry }

type Metadata struct {
	Version         string `json:"version"`
	Source          string `json:"source"`
	SourceSHA256    string `json:"source_sha256"`
	CatalogSHA256   string `json:"catalog_sha256"`
	SourceRows      int    `json:"source_rows"`
	ModelBrandPairs int    `json:"model_brand_pairs"`
}

var shared = sync.OnceValue(func() *Catalog {
	catalog, err := load(strings.NewReader(catalogCSV))
	if err != nil {
		panic(fmt.Sprintf("invalid embedded UA device catalog: %v", err))
	}
	return catalog
})

// DefaultCatalog loads once at startup and is safe for concurrent read-only lookups.
func DefaultCatalog() *Catalog { return shared() }

func Version() Metadata {
	var metadata Metadata
	if err := json.Unmarshal([]byte(catalogMetadata), &metadata); err != nil {
		panic(err)
	}
	return metadata
}

func load(reader io.Reader) (*Catalog, error) {
	r := csv.NewReader(reader)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	if len(header) != 2 || header[0] != "model" || header[1] != "brand" {
		return nil, fmt.Errorf("expected model,brand CSV")
	}
	c := &Catalog{models: make(map[string]modelEntry, 45000)}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		model, brand := strings.TrimSpace(row[0]), NormalizeBrand(row[1])
		if model == "" || brand == "" {
			continue
		}
		key := strings.ToLower(model)
		device := Device{Brand: brand, Model: model}
		entry, found := c.models[key]
		if !found {
			c.models[key] = modelEntry{device: device}
			continue
		}
		if entry.device.Brand == brand {
			continue
		}
		if len(entry.alternatives) == 0 {
			entry.alternatives = []Device{entry.device}
		}
		duplicate := false
		for _, candidate := range entry.alternatives {
			if candidate.Brand == brand {
				duplicate = true
				break
			}
		}
		if !duplicate {
			entry.alternatives = append(entry.alternatives, device)
		}
		c.models[key] = entry
	}
	if len(c.models) == 0 {
		return nil, fmt.Errorf("empty device catalog")
	}
	return c, nil
}

// LookupModel only resolves a cross-brand collision with independent evidence.
// A generic hint or an unsupported hint never chooses an arbitrary brand.
func (c *Catalog) LookupModel(model, brandHint string) (Device, bool) {
	if c == nil {
		return Device{}, false
	}
	entry, found := c.models[strings.ToLower(strings.TrimSpace(model))]
	if !found {
		return Device{}, false
	}
	if len(entry.alternatives) == 0 {
		return entry.device, true
	}
	hint := NormalizeBrand(brandHint)
	if hint == "" {
		return Device{}, false
	}
	for _, candidate := range entry.alternatives {
		if candidate.Brand == hint {
			return candidate, true
		}
	}
	return Device{}, false
}

// LookupUA has no regular expressions; model extraction is bounded by UA length.
func (c *Catalog) LookupUA(raw, brandHint string) (Device, bool) {
	model := AndroidModel(raw)
	if model == "" || model == "K" {
		return Device{}, false
	}
	return c.LookupModel(model, brandHint)
}

// AndroidModel extracts a model token, not a marketing name. Build/ and the
// AppleWebKit delimiter keep parentheses in models such as moto g(30) intact.
func AndroidModel(raw string) string {
	start := strings.Index(raw, "Android ")
	if start < 0 {
		return ""
	}
	text := raw[start:]
	if end := strings.Index(text, " Build/"); end >= 0 {
		text = text[:end]
	} else if end = strings.Index(text, ") AppleWebKit"); end >= 0 {
		text = text[:end]
	} else if end = strings.IndexByte(text, ')'); end >= 0 {
		text = text[:end]
	}
	for {
		semicolon := strings.LastIndexByte(text, ';')
		if semicolon < 0 {
			return ""
		}
		token := strings.TrimSpace(text[semicolon+1:])
		text = text[:semicolon]
		if token == "" || token == "wv" || localeToken(token) || strings.HasPrefix(token, "MIUI/") {
			continue
		}
		if at := strings.Index(token, " MIUI/"); at >= 0 {
			token = strings.TrimSpace(token[:at])
		}
		return token
	}
}

// Some WebViews put the locale after the model (e.g. BRP-NX1; en-AU).
// Only skip the unambiguous language-country form, not model-code prefixes.
func localeToken(value string) bool {
	separator := strings.IndexAny(value, "-_")
	if (separator != 2 && separator != 3) || len(value) != separator+3 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if i == separator {
			continue
		}
		c := value[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') {
			return false
		}
	}
	return true
}

// These checks are on the Engine cache-hit path; EqualFold avoids allocating
// lowercase copies of otherwise already known makes and model codes.
func UnknownBrand(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "other") || strings.EqualFold(value, "unknown") || strings.EqualFold(value, "generic") {
		return true
	}
	return len(value) > 7 && (value[7] == '_' || value[7] == ' ') && strings.EqualFold(value[:7], "generic")
}

func UnknownModel(value string) bool {
	value = strings.TrimSpace(value)
	return UnknownBrand(value) || strings.EqualFold(value, "k") || strings.EqualFold(value, "smartphone") || strings.EqualFold(value, "tablet")
}

// NormalizeBrand standardizes aliases without merging Redmi/POCO/iQOO into
// their parent brands. Unknown values are empty rather than invented makes.
func NormalizeBrand(value string) string {
	value = strings.TrimSpace(value)
	if UnknownBrand(value) {
		return ""
	}
	switch strings.ToLower(value) {
	case "samsung":
		return "Samsung"
	case "xiaomi":
		return "Xiaomi"
	case "vivo":
		return "vivo"
	case "iqoo":
		return "iQOO"
	case "poco":
		return "POCO"
	case "redmi":
		return "Redmi"
	case "motorola":
		return "Motorola"
	case "oppo":
		return "OPPO"
	case "realme":
		return "realme"
	case "lge", "lg":
		return "LG"
	case "honor":
		return "Honor"
	}
	return value
}
