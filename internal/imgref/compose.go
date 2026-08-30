package imgref

import (
	"bytes"

	"github.com/google/go-containerregistry/pkg/name"
	"gopkg.in/yaml.v3"
)

// ScanCompose finds every services.<name>.image value in a Compose file.
//
// This walks yaml.Node rather than using compose-go, for two reasons. Node
// keeps Line and Column, which a rewrite needs, and compose-go resolves
// variables and validates the document, so what it hands back no longer
// corresponds to the bytes on disk.
func ScanCompose(path string, data []byte) ([]Ref, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return nil, nil
	}

	services := mappingValue(doc.Content[0], "services")
	if services == nil || services.Kind != yaml.MappingNode {
		return nil, nil
	}

	li := newLineIndex(data)
	var refs []Ref
	// A mapping's Content alternates key, value, key, value.
	for i := 0; i+1 < len(services.Content); i += 2 {
		serviceName := services.Content[i].Value
		image := mappingValue(services.Content[i+1], "image")
		if image == nil || image.Kind != yaml.ScalarNode {
			continue
		}
		refs = append(refs, composeRef(path, data, li, serviceName, image))
	}
	return refs, nil
}

func composeRef(path string, data []byte, li lineIndex, service string, node *yaml.Node) Ref {
	raw := node.Value
	ref := Ref{
		Path:  path,
		Line:  node.Line,
		Kind:  KindComposeImage,
		Raw:   raw,
		Stage: service,
	}
	if bytes.ContainsAny([]byte(raw), "$") {
		ref.Note = "depends on a variable"
		return ref
	}
	parsed, err := name.ParseReference(raw)
	if err != nil {
		ref.Note = err.Error()
		return ref
	}
	fill(&ref, parsed)
	if start, end, ok := li.offsetOf(data, node.Line, raw); ok {
		ref.Start, ref.End = start, end
		ref.Resolved = true
	} else {
		ref.Note = "could not locate the reference on its line"
	}
	return ref
}

// mappingValue returns the value node for key in a mapping, or nil.
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.DocumentNode && len(node.Content) > 0 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
