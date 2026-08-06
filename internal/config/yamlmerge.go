package config

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// yamlDocument is a parsed YAML file kept as a node tree rather than decoded
// into Go structs.
//
// The node tree is what makes a comment-preserving merge possible. A real
// hermes config.yaml is overwhelmingly documentation -- one observed file was
// 1246 lines of which 1044 were comments -- so decoding to a struct and
// re-encoding would delete the file's entire explanatory content along with
// every key the struct does not model. Hermes' own `hermes config set` does
// exactly that, which is why OneAgent does not shell out to it.
type yamlDocument struct {
	root yaml.Node
	// baseline is the document rendered before any edit, used to decide whether a
	// write would change anything.
	//
	// It is deliberately the re-rendered form rather than the file as read. The
	// encoder has its own house style -- it indents sequence entries under their
	// key, while Hermes and PyYAML write them flush left, and it re-emits a
	// comment trailing a nested mapping one level deeper. Comparing against the
	// raw file would read those as changes and rewrite the file on every pass,
	// re-applying the same cosmetic shift each time. Comparing against the
	// baseline means only a real value change reaches the disk, so a config whose
	// values already match is never touched at all.
	baseline string
}

func parseYAML(text string) (*yamlDocument, error) {
	document := &yamlDocument{}
	if strings.TrimSpace(text) == "" {
		document.root = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{mappingNode()}}
		document.captureBaseline()
		return document, nil
	}
	if err := yaml.Unmarshal([]byte(text), &document.root); err != nil {
		return nil, err
	}
	// A file holding only comments parses to a node with no content and, because
	// there was no document body, no kind either. Both have to be set or the
	// encoder rejects the tree; setting Content alone leaves kind 0.
	if len(document.root.Content) == 0 {
		document.root.Kind = yaml.DocumentNode
		document.root.Content = []*yaml.Node{mappingNode()}
	}
	if document.root.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("top level must be a mapping, found %s", nodeKindName(document.root.Content[0].Kind))
	}
	document.captureBaseline()
	return document, nil
}

// captureBaseline records what this document renders to before any edit. A
// failure to render is ignored: it leaves the baseline empty, which can only
// make a later write happen when it might have been skipped, never the reverse.
func (d *yamlDocument) captureBaseline() {
	if rendered, err := d.Marshal(); err == nil {
		d.baseline = string(rendered)
	}
}

func mappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func nodeKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.SequenceNode:
		return "a sequence"
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "an unexpected node"
	}
}

func (d *yamlDocument) top() *yaml.Node { return d.root.Content[0] }

// unchanged reports whether the edits changed any value. Compared against the
// pre-edit rendering, so the encoder's own formatting never counts as a change.
func (d *yamlDocument) unchanged(rendered []byte) bool {
	return d.baseline != "" && d.baseline == string(rendered)
}

// mappingEntry returns the value node for key, or nil when absent. A YAML
// mapping's Content alternates key, value, which is why this steps by two
// rather than using a map: the order is the file's own and must survive.
func mappingEntry(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// childMapping returns the mapping stored at key, creating it when missing.
//
// An existing key whose value is not a mapping is an error rather than
// something to overwrite: the user put something else there, and replacing it
// would discard their content silently.
func childMapping(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if existing := mappingEntry(mapping, key); existing != nil {
		// A key written as `model:` with nothing after it parses as a null
		// scalar. Hermes ships exactly that as its unconfigured sentinel, so it
		// is promoted to a mapping instead of being rejected.
		if existing.Kind == yaml.ScalarNode && (existing.Tag == "!!null" || existing.Value == "") {
			existing.Kind = yaml.MappingNode
			existing.Tag = "!!map"
			existing.Value = ""
			return existing, nil
		}
		if existing.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("%q must contain a mapping, found %s", key, nodeKindName(existing.Kind))
		}
		return existing, nil
	}
	created := mappingNode()
	mapping.Content = append(mapping.Content, scalarNode(key), created)
	return created, nil
}

// setScalar assigns key = value, replacing an existing value in place so any
// comment attached to that line survives.
func setScalar(mapping *yaml.Node, key, value string) {
	if existing := mappingEntry(mapping, key); existing != nil {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		// A value that came from a quoted or folded scalar keeps its original
		// style otherwise, which can re-emit the new value wrapped or quoted
		// unexpectedly.
		existing.Style = 0
		existing.Content = nil
		return
	}
	mapping.Content = append(mapping.Content, scalarNode(key), scalarNode(value))
}

// childSequence returns the sequence stored at key, creating it when missing.
func childSequence(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if existing := mappingEntry(mapping, key); existing != nil {
		if existing.Kind == yaml.ScalarNode && (existing.Tag == "!!null" || existing.Value == "") {
			existing.Kind = yaml.SequenceNode
			existing.Tag = "!!seq"
			existing.Value = ""
			return existing, nil
		}
		if existing.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("%q must contain a sequence, found %s", key, nodeKindName(existing.Kind))
		}
		return existing, nil
	}
	created := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	mapping.Content = append(mapping.Content, scalarNode(key), created)
	return created, nil
}

// sequenceEntryByName finds a mapping in a sequence whose "name" is value. This
// is how Hermes identifies a custom provider, so an update has to match on it
// rather than on position: the user may have reordered the list.
func sequenceEntryByName(sequence *yaml.Node, name string) *yaml.Node {
	for _, item := range sequence.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if key := mappingEntry(item, "name"); key != nil && key.Value == name {
			return item
		}
	}
	return nil
}

// Marshal renders the document. Indentation is pinned to two spaces because the
// encoder defaults to four, which would reformat every nested line in a file
// OneAgent only meant to touch a few keys of.
func (d *yamlDocument) Marshal() ([]byte, error) {
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(&d.root); err != nil {
		encoder.Close()
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
