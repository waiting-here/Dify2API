// Package difyapp hosts the committed Dify App DSL templates and the
// server-side obfuscation generator used by downloadable-template services
// (v1.4.0: sillytavern-main-v1). Builds never depend on the git-excluded
// dify_app/ directory.
package difyapp

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed assets/sillytavern-main-200.yml
var templateSillytavern200 []byte

// Template describes one downloadable Dify App template.
type Template struct {
	// Service is the translator service name this template serves.
	Service string
	// Asset is the committed DSL content.
	Asset []byte
	// CanonicalVariables is the ordered contract variable list of the
	// template's start node (the same set the translator contract sends).
	CanonicalVariables []string
}

// ModelOptions are the admin-managed portions of a template's model node and
// marketplace dependency pin. ParamsJSON must decode to a JSON object; its
// keys replace completion_params in the generated DSL.
type ModelOptions struct {
	ModelKey         string
	Provider         string
	DependencyPlugin string
	DependencyVer    string
	DependencyHash   string
	ParamsJSON       string
}

// Templates is the registry of downloadable templates (one per service).
var Templates = map[string]*Template{
	"sillytavern-main-v1": {
		Service: "sillytavern-main-v1",
		Asset:   templateSillytavern200,
	},
}

// TemplateFor returns the template registered for a service, or nil.
func TemplateFor(service string) *Template {
	t, ok := Templates[service]
	if !ok {
		return nil
	}
	return t
}

// ServiceNames lists services with a downloadable template.
func ServiceNames() []string {
	out := make([]string, 0, len(Templates))
	for name := range Templates {
		out = append(out, name)
	}
	return out
}

// ApplyModelConfig clones a template DSL and replaces the marketplace
// dependency pin plus the first LLM node's model/provider/completion_params.
// This happens before obfuscation so the downloaded DSL always reflects the
// model selected in the user dialog and the latest admin/marketplace values.
func ApplyModelConfig(base []byte, options ModelOptions) ([]byte, error) {
	modelKey := strings.TrimSpace(options.ModelKey)
	provider := strings.TrimSpace(options.Provider)
	plugin := strings.TrimSpace(options.DependencyPlugin)
	version := strings.TrimSpace(options.DependencyVer)
	hash := strings.TrimSpace(options.DependencyHash)
	if modelKey == "" || provider == "" || plugin == "" || version == "" || hash == "" {
		return nil, fmt.Errorf("difyapp: incomplete model configuration")
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, fmt.Errorf("difyapp: parse template for model config: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("difyapp: invalid template document")
	}
	root := doc.Content[0]

	dependencies, ok := seqChild(root, "dependencies")
	if !ok || len(dependencies.Content) == 0 {
		return nil, fmt.Errorf("difyapp: marketplace dependency missing")
	}
	dependencyValue, ok := mapChild(dependencies.Content[0], "value")
	if !ok || dependencyValue.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("difyapp: marketplace dependency value missing")
	}
	setMapScalar(dependencyValue, "marketplace_plugin_unique_identifier", plugin+":"+version+"@"+hash)

	workflow, ok := mapChild(root, "workflow")
	if !ok {
		return nil, fmt.Errorf("difyapp: workflow block missing")
	}
	graph, ok := mapChild(workflow, "graph")
	if !ok {
		return nil, fmt.Errorf("difyapp: workflow graph missing")
	}
	nodes, ok := seqChild(graph, "nodes")
	if !ok {
		return nil, fmt.Errorf("difyapp: workflow nodes missing")
	}
	var llmModel *yaml.Node
	for _, node := range nodes.Content {
		data, ok := mapChild(node, "data")
		if !ok {
			continue
		}
		if typ, _ := mapScalar(data, "type"); typ != "llm" {
			continue
		}
		llmModel, ok = mapChild(data, "model")
		if ok && llmModel.Kind == yaml.MappingNode {
			break
		}
		llmModel = nil
	}
	if llmModel == nil {
		return nil, fmt.Errorf("difyapp: LLM model node missing")
	}
	setMapScalar(llmModel, "name", modelKey)
	providerID := provider
	if !strings.Contains(providerID, "/") {
		providerID = plugin + "/" + providerID
	}
	setMapScalar(llmModel, "provider", providerID)

	if strings.TrimSpace(options.ParamsJSON) != "" {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(options.ParamsJSON), &params); err != nil {
			return nil, fmt.Errorf("difyapp: parse model params_json: %w", err)
		}
		if params == nil {
			return nil, fmt.Errorf("difyapp: params_json must be an object")
		}
		paramsNode, err := yamlValueNode(params)
		if err != nil {
			return nil, fmt.Errorf("difyapp: encode model params_json: %w", err)
		}
		if paramsNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("difyapp: params_json must be an object")
		}
		setMapNode(llmModel, "completion_params", paramsNode)
	}

	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("difyapp: encode configured template: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return []byte(sb.String()), nil
}

func yamlValueNode(value interface{}) (*yaml.Node, error) {
	var doc yaml.Node
	data, err := yaml.Marshal(value)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("invalid YAML value")
	}
	return doc.Content[0], nil
}

func setMapNode(m *yaml.Node, key string, value *yaml.Node) {
	if m == nil || m.Kind != yaml.MappingNode || value == nil {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

// extractStartVariables reads the start node's variable names from a parsed
// document. Returns the ordered list.
func extractStartVariables(doc *yaml.Node) ([]string, error) {
	if doc.Kind == yaml.DocumentNode && len(doc.Content) == 1 {
		doc = doc.Content[0]
	}
	workflow, ok := mapChild(doc, "workflow")
	if !ok {
		return nil, fmt.Errorf("difyapp: workflow block missing")
	}
	graph, ok := mapChild(workflow, "graph")
	if !ok {
		return nil, fmt.Errorf("difyapp: graph block missing")
	}
	nodes, ok := seqChild(graph, "nodes")
	if !ok {
		return nil, fmt.Errorf("difyapp: graph.nodes missing")
	}
	for _, node := range nodes.Content {
		if node.Kind != yaml.MappingNode {
			continue
		}
		data, ok := mapChild(node, "data")
		if !ok {
			continue
		}
		if nodeType, ok := mapScalar(data, "type"); !ok || nodeType != "start" {
			continue
		}
		vars, ok := seqChild(data, "variables")
		if !ok {
			return nil, fmt.Errorf("difyapp: start node has no variables")
		}
		out := make([]string, 0, len(vars.Content))
		for _, v := range vars.Content {
			if v.Kind != yaml.MappingNode {
				continue
			}
			name, ok := mapScalar(v, "variable")
			if !ok {
				return nil, fmt.Errorf("difyapp: start variable entry missing 'variable' key")
			}
			out = append(out, name)
		}
		return out, nil
	}
	return nil, fmt.Errorf("difyapp: start node not found")
}

// ExtractStartVariables parses DSL bytes and returns the start node's
// ordered variable names.
func ExtractStartVariables(dsl []byte) ([]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(dsl, &doc); err != nil {
		return nil, fmt.Errorf("difyapp: parse template: %w", err)
	}
	return extractStartVariables(&doc)
}

// mapChild returns the mapping child with the given key.
func mapChild(parent *yaml.Node, key string) (*yaml.Node, bool) {
	if parent == nil || parent.Kind != yaml.MappingNode {
		return nil, false
	}
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1], true
		}
	}
	return nil, false
}

// seqChild returns the sequence child with the given key.
func seqChild(parent *yaml.Node, key string) (*yaml.Node, bool) {
	c, ok := mapChild(parent, key)
	if !ok || c.Kind != yaml.SequenceNode {
		return nil, false
	}
	return c, true
}

// mapScalar returns the scalar value of the given mapping key.
func mapScalar(parent *yaml.Node, key string) (string, bool) {
	c, ok := mapChild(parent, key)
	if !ok || c.Kind != yaml.ScalarNode {
		return "", false
	}
	return c.Value, true
}

// hasCanonicalLeak reports whether any scalar in the document still contains
// a canonical variable name or an original node id (self-validation).
func hasCanonicalLeak(n *yaml.Node, canonical []string, origIDs map[string]bool) (leak string) {
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		v := n.Value
		for _, name := range canonical {
			if v == name || strings.Contains(v, "."+name+"#") || strings.Contains(v, "#"+name) {
				return name
			}
		}
		if origIDs[v] {
			return v
		}
	case yaml.MappingNode, yaml.SequenceNode:
		for _, c := range n.Content {
			if leak := hasCanonicalLeak(c, canonical, origIDs); leak != "" {
				return leak
			}
		}
	}
	return ""
}
