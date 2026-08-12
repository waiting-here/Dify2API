package difyapp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ObfuscationResult is one generated obfuscated DSL bundle.
type ObfuscationResult struct {
	// DSL is the obfuscated, self-validated template (importable by Dify).
	DSL []byte
	// Mapping maps canonical contract variables to obfuscated keys. Dummy
	// variables are not listed here — callers must ignore unmapped keys.
	Mapping map[string]string
	// DummyCount and DummyKeys describe the extra placeholder variables.
	// Dummy keys are exported with generation records but never routed.
	DummyCount int
	DummyKeys  []string
	// Seed is the random seed used for this generation (persisted for
	// record-keeping; the mapping is authoritative for routing).
	Seed []byte
}

var (
	// templateRef matches Dify template references like {{#2000000000001.user_0#}}.
	templateRef = regexp.MustCompile(`\{\{#([0-9a-zA-Z_\-]+)\.([0-9a-zA-Z_\-]+)#\}\}`)
	// plainRef matches bare #node.var# occurrences.
	plainRef = regexp.MustCompile(`#([0-9a-zA-Z_\-]+)\.([0-9a-zA-Z_\-]+)#`)
)

// randomHex returns n random hex chars.
func randomHex(n int) (string, error) {
	b := make([]byte, (n+1)/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b)[:n], nil
}

// randInt returns a uniform integer in [0, max).
func randInt(max int) (int, error) {
	if max <= 0 {
		return 0, nil
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}

// dummyTitle returns a plausible variable label for a dummy slot.
func dummyTitle(i int) string {
	words := []string{"note", "hint", "extra", "detail", "context", "remark", "fragment", "supplement"}
	idx := i % len(words)
	return fmt.Sprintf("%s %d", words[idx], i+1)
}

// GenerateObfuscated produces a fresh obfuscated DSL from the base template
// with a per-download random seed: renamed variable keys and labels, renamed
// node ids (all references synchronized), random app name/description/node
// titles, and a random count of dummy variables. The output is parsed again
// and structurally validated before it is returned.
func GenerateObfuscated(base []byte, seed []byte) (*ObfuscationResult, error) {
	if len(seed) == 0 {
		seed = make([]byte, 16)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(base, &doc); err != nil {
		return nil, fmt.Errorf("difyapp: parse base template: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return nil, fmt.Errorf("difyapp: invalid base document")
	}
	root := doc.Content[0]

	canonical, err := extractStartVariables(root)
	if err != nil {
		return nil, err
	}

	// --- 1. Rename the app identity -----------------------------------
	appNode, ok := mapChild(root, "app")
	if !ok || appNode.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("difyapp: app block missing")
	}
	randHex, err := randomHex(6)
	if err != nil {
		return nil, err
	}
	setMapScalar(appNode, "name", "Chat Workspace "+randHex)
	setMapScalar(appNode, "description", "Dify2API generated workflow (v1).")
	setMapScalar(appNode, "icon", "🎣")

	// --- 2. Rename node ids (deterministic fresh ids, no collisions) ----
	workflow, _ := mapChild(root, "workflow")
	graph, _ := mapChild(workflow, "graph")
	nodes, _ := seqChild(graph, "nodes")
	origIDs := make(map[string]bool)
	idMap := make(map[string]string)
	newID := func() (string, error) {
		h, err := randomHex(10)
		if err != nil {
			return "", err
		}
		return h, nil
	}
	collectNodeIDs(nodes, origIDs, idMap, newID)
	if len(idMap) == 0 {
		return nil, fmt.Errorf("difyapp: no node ids found")
	}
	startID := ""
	for _, node := range nodes.Content {
		if data, ok := mapChild(node, "data"); ok {
			if t, ok := mapScalar(data, "type"); ok && t == "start" {
				startID, _ = mapScalar(node, "id")
				break
			}
		}
	}
	_ = startID

	// --- 3. Rename start variables + labels; add dummies ----------------
	startData, _ := mapChild(firstNodeOfType(nodes, "start"), "data")
	varsSeq, _ := seqChild(startData, "variables")
	keyMap := make(map[string]string, len(canonical))
	for i, name := range canonical {
		key, err := obfuscatedKey(name, seed, i)
		if err != nil {
			return nil, err
		}
		keyMap[name] = key
	}
	for _, v := range varsSeq.Content {
		if v.Kind != yaml.MappingNode {
			continue
		}
		oldName, ok := mapScalar(v, "variable")
		if !ok {
			continue
		}
		newName := keyMap[oldName]
		if newName == "" {
			continue
		}
		setMapScalar(v, "variable", newName)
		// Generic label; hints/placeholders cleared (they may repeat the
		// canonical name in Chinese).
		setMapScalar(v, "label", "field_"+newName)
		setMapScalar(v, "hint", "")
		setMapScalar(v, "placeholder", "")
	}

	// --- 4. Dummies (random count, random keys, optional) ----------------
	dummyCount, err := randInt(11)
	if err != nil {
		return nil, err
	}
	dummyCount += 5 // 5..15
	dummyKeys := make(map[string]bool)
	for i := 0; i < dummyCount; i++ {
		key, err := randomHex(10)
		if err != nil {
			return nil, err
		}
		key = "x_" + key
		if dummyKeys[key] {
			i--
			continue
		}
		dummyKeys[key] = true
		varsSeq.Content = append(varsSeq.Content, &yaml.Node{
			Kind: yaml.MappingNode, Tag: "!!map",
			Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "default"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "hint"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "label"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: dummyTitle(i)},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "options"},
				{Kind: yaml.SequenceNode, Tag: "!!seq"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "placeholder"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: ""},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "required"},
				{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "false"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "type"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "paragraph"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: "variable"},
				{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
			},
		})
	}

	// --- 5. Rewrite all references across the document -------------------
	rewriteScalars(root, func(v string) string {
		out := templateRef.ReplaceAllStringFunc(v, func(m string) string {
			sub := templateRef.FindStringSubmatch(m)
			return "{{#" + idMap[sub[1]] + "." + keyMap[sub[2]] + "#}}"
		})
		out = plainRef.ReplaceAllStringFunc(out, func(m string) string {
			sub := plainRef.FindStringSubmatch(m)
			if idMap[sub[1]] == "" {
				return m
			}
			newVar := keyMap[sub[2]]
			if newVar == "" {
				return m
			}
			return "#" + idMap[sub[1]] + "." + newVar + "#"
		})
		// Exact node-id / canonical-variable scalars (variable_selector
		// entries, any leftover bare references).
		if idMap[out] != "" {
			return idMap[out]
		}
		if keyMap[out] != "" {
			return keyMap[out]
		}
		// Edge ids embed node ids ("<id>-source-<id>-target"); rewrite the
		// embedded occurrences so no original id survives anywhere.
		for oldID, newID := range idMap {
			if strings.Contains(out, oldID) {
				out = strings.ReplaceAll(out, oldID, newID)
			}
		}
		return out
	})

	// --- 6. Rename node titles -------------------------------------------
	titleWords := []string{"handler", "stage", "module", "channel", "relay", "segment", "unit"}
	for _, node := range nodes.Content {
		if data, ok := mapChild(node, "data"); ok {
			if t, ok := mapScalar(data, "type"); ok {
				if t == "start" {
					setMapScalar(data, "title", "Entry")
					continue
				}
				idx, _ := randInt(len(titleWords))
				hexPart, _ := randomHex(4)
				setMapScalar(data, "title", titleWords[idx]+"_"+hexPart)
				// desc comments may reference variables; blank them.
				if _, ok := mapChild(data, "desc"); ok {
					setMapScalar(data, "desc", "")
				}
			}
		}
	}

	// --- 7. Encode + self-validate ---------------------------------------
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, fmt.Errorf("difyapp: encode obfuscated template: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := []byte(sb.String())

	var check yaml.Node
	if err := yaml.Unmarshal(out, &check); err != nil {
		return nil, fmt.Errorf("difyapp: self-validation parse failed: %w", err)
	}
	checkRoot := check.Content[0]
	checkVars, err := extractStartVariables(checkRoot)
	if err != nil {
		return nil, fmt.Errorf("difyapp: self-validation structure failed: %w", err)
	}
	if len(checkVars) != len(canonical)+dummyCount {
		return nil, fmt.Errorf("difyapp: self-validation variable count %d, want %d", len(checkVars), len(canonical)+dummyCount)
	}
	if leak := hasCanonicalLeak(checkRoot, canonical, origIDs); leak != "" {
		return nil, fmt.Errorf("difyapp: self-validation found leftover %q", leak)
	}

	dummyList := make([]string, 0, len(dummyKeys))
	for key := range dummyKeys {
		dummyList = append(dummyList, key)
	}
	sort.Strings(dummyList)
	return &ObfuscationResult{
		DSL:        out,
		Mapping:    keyMap,
		DummyCount: dummyCount,
		DummyKeys:  dummyList,
		Seed:       seed,
	}, nil
}

// obfuscatedKey derives a random-looking key for a canonical variable from
// the per-download seed: the same seed reproduces the same mapping, while
// fresh seeds (every download) yield entirely different keys.
func obfuscatedKey(canonical string, seed []byte, i int) (string, error) {
	h := sha256.New()
	h.Write(seed)
	h.Write([]byte{0x1f})
	h.Write([]byte(canonical))
	return "v_" + hex.EncodeToString(h.Sum(nil))[:10], nil
}

// collectNodeIDs walks the graph nodes, maps every node "id" to a fresh id,
// and records the originals.
func collectNodeIDs(nodes *yaml.Node, origIDs map[string]bool, idMap map[string]string, newID func() (string, error)) {
	if nodes == nil {
		return
	}
	for _, node := range nodes.Content {
		if node.Kind != yaml.MappingNode {
			continue
		}
		id, ok := mapScalar(node, "id")
		if !ok || id == "" {
			continue
		}
		if idMap[id] != "" {
			continue
		}
		origIDs[id] = true
		nid, err := newID()
		if err != nil {
			continue
		}
		idMap[id] = nid
		setMapScalar(node, "id", nid)
	}
}

// firstNodeOfType returns the first graph node whose data.type matches.
func firstNodeOfType(nodes *yaml.Node, typ string) *yaml.Node {
	if nodes == nil {
		return nil
	}
	for _, node := range nodes.Content {
		if data, ok := mapChild(node, "data"); ok {
			if t, ok := mapScalar(data, "type"); ok && t == typ {
				return node
			}
		}
	}
	return nil
}

// rewriteScalars applies fn to every scalar value in the document (used for
// reference rewriting; node keys are left untouched). variable_selector
// sequences ([node_id, variable_name]) are rewritten in place.
func rewriteScalars(n *yaml.Node, fn func(string) string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!str" {
			n.Value = fn(n.Value)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			keyNode, valNode := n.Content[i], n.Content[i+1]
			// variable_selector: [node_id, variable_name]
			if keyNode.Value == "variable_selector" && valNode.Kind == yaml.SequenceNode &&
				len(valNode.Content) == 2 &&
				valNode.Content[0].Kind == yaml.ScalarNode && valNode.Content[1].Kind == yaml.ScalarNode {
				valNode.Content[0].Value = fn(valNode.Content[0].Value)
				valNode.Content[1].Value = fn(valNode.Content[1].Value)
				continue
			}
			rewriteScalars(valNode, fn)
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			rewriteScalars(c, fn)
		}
	}
}

// setMapScalar sets or adds a scalar value for a mapping key.
func setMapScalar(m *yaml.Node, key, value string) {
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1].Value = value
			m.Content[i+1].Tag = "!!str"
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

// SortedMappingKeys returns the obfuscated keys of a mapping (test helper).
func SortedMappingKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
