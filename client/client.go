// Package client is the client registry: read/list/get + comment-preserving
// add/edit/remove on the shared `clients:` map (see package config for where that
// map physically lives). Writes edit the YAML via the node API — like `yq -i` — so
// human comments in the shared team file survive edits.
package client

import (
	"bytes"
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"

	"mandos/config"
)

// Client is the typed view of one entry in the `clients:` map. Core fields are
// promoted; any other keys (e.g. deactivate_plugins, review_pages — tool-specific)
// are preserved in Extra so a round-trip never drops them.
type Client struct {
	Name        string         `yaml:"-"`
	SSH         string         `yaml:"ssh,omitempty"`
	WPRoot      string         `yaml:"wp_root,omitempty"`
	RemoteTmp   string         `yaml:"remote_tmp,omitempty"`
	LocalHost   string         `yaml:"local_host,omitempty"`
	CloudDir    string         `yaml:"cloud_dir,omitempty"`
	CloudFolder string         `yaml:"cloud_folder,omitempty"`
	Domain      string         `yaml:"domain,omitempty"`
	Extra       map[string]any `yaml:",inline"`
}

// nameRe mirrors wpsite's _valid_site_name: a DNS label — lowercase letters, digits
// and hyphens, not starting or ending with a hyphen.
var nameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// ValidName reports whether name is a DNS-label-safe client name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

// List returns client names in the order they appear in the file.
func List() ([]string, error) {
	f, err := config.ClientFileRead()
	if err != nil {
		return nil, err
	}
	_, cn, err := clientsMapping(f, false)
	if err != nil {
		return nil, err
	}
	names := []string{}
	if cn == nil {
		return names, nil
	}
	for i := 0; i+1 < len(cn.Content); i += 2 {
		names = append(names, cn.Content[i].Value)
	}
	return names, nil
}

// Has reports whether a client with this name exists.
func Has(name string) (bool, error) {
	names, err := List()
	if err != nil {
		return false, err
	}
	for _, n := range names {
		if n == name {
			return true, nil
		}
	}
	return false, nil
}

// Get returns the typed client, or an error if it doesn't exist.
func Get(name string) (*Client, error) {
	f, err := config.ClientFileRead()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("client %q not found", name)
		}
		return nil, err
	}
	var reg struct {
		Clients map[string]Client `yaml:"clients"`
	}
	if err := yaml.Unmarshal(b, &reg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", f, err)
	}
	c, ok := reg.Clients[name]
	if !ok {
		return nil, fmt.Errorf("client %q not found", name)
	}
	c.Name = name
	return &c, nil
}

// GetMap returns one client as a generic map (for JSON output / arbitrary fields).
func GetMap(name string) (map[string]any, error) {
	f, err := config.ClientFileRead()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(f)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("client %q not found", name)
		}
		return nil, err
	}
	var reg struct {
		Clients map[string]map[string]any `yaml:"clients"`
	}
	if err := yaml.Unmarshal(b, &reg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", f, err)
	}
	c, ok := reg.Clients[name]
	if !ok {
		return nil, fmt.Errorf("client %q not found", name)
	}
	return c, nil
}

// GetField reads one scalar field of a client ("" if the field is absent).
func GetField(name, key string) (string, error) {
	f, err := config.ClientFileRead()
	if err != nil {
		return "", err
	}
	_, cn, err := clientsMapping(f, false)
	if err != nil {
		return "", err
	}
	cm := mapGet(cn, name)
	if cm == nil {
		return "", fmt.Errorf("client %q not found", name)
	}
	v := mapGet(cm, key)
	if v == nil {
		return "", nil
	}
	if v.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("field %q of %q is not a scalar", key, name)
	}
	return v.Value, nil
}

// Set writes one scalar field, creating the client if needed. Comment-preserving.
func Set(name, key, value string) error {
	f, err := config.ClientFileWrite()
	if err != nil {
		return err
	}
	doc, cn, err := clientsMapping(f, true)
	if err != nil {
		return err
	}
	cm := mapGet(cn, name)
	if cm == nil {
		cm = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapSet(cn, name, cm)
	}
	mapSet(cm, key, scalar(value))
	return writeDoc(f, doc)
}

// Unset removes one field from a client (no-op if absent). Comment-preserving.
func Unset(name, key string) error {
	f, err := config.ClientFileWrite()
	if err != nil {
		return err
	}
	doc, cn, err := clientsMapping(f, false)
	if err != nil {
		return err
	}
	cm := mapGet(cn, name)
	if cm == nil {
		return nil
	}
	mapDelete(cm, key)
	return writeDoc(f, doc)
}

// Remove deletes a client entirely. Comment-preserving.
func Remove(name string) error {
	f, err := config.ClientFileWrite()
	if err != nil {
		return err
	}
	doc, cn, err := clientsMapping(f, false)
	if err != nil {
		return err
	}
	if cn == nil || !mapDelete(cn, name) {
		return fmt.Errorf("client %q not found", name)
	}
	return writeDoc(f, doc)
}

// ---------------------------------------------------------------------------
// YAML node helpers — surgical edits that preserve surrounding comments.
// ---------------------------------------------------------------------------

// clientsMapping loads a file and returns its document node plus the `clients:`
// mapping node. With create=true it materialises an empty document / clients map as
// needed; with create=false a missing file or missing clients key yields a nil node.
func clientsMapping(path string, create bool) (*yaml.Node, *yaml.Node, error) {
	doc, err := loadDoc(path)
	if err != nil {
		return nil, nil, err
	}
	top := topMapping(doc, create)
	if top == nil {
		return doc, nil, nil
	}
	cn := mapGet(top, "clients")
	if cn == nil {
		if !create {
			return doc, nil, nil
		}
		cn = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		mapSet(top, "clients", cn)
	}
	return doc, cn, nil
}

func loadDoc(path string) (*yaml.Node, error) {
	var doc yaml.Node
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &doc, nil // empty document
		}
		return nil, err
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &doc, nil
}

// topMapping returns the document's root mapping node, creating an empty one when the
// document is empty and create is set.
func topMapping(doc *yaml.Node, create bool) *yaml.Node {
	if len(doc.Content) == 0 {
		if !create {
			return nil
		}
		m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{m}
		return m
	}
	return doc.Content[0]
}

// mapGet returns the value node for key in a mapping node, or nil.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// mapSet sets key=val in a mapping, replacing an existing value or appending a new
// key/value pair (which keeps existing entries and their comments intact).
func mapSet(m *yaml.Node, key string, val *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = val
			return
		}
	}
	m.Content = append(m.Content, scalar(key), val)
}

// mapDelete removes key (and its value) from a mapping. Returns whether it existed.
func mapDelete(m *yaml.Node, key string) bool {
	if m == nil {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

func scalar(v string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v}
}

func writeDoc(path string, doc *yaml.Node) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
