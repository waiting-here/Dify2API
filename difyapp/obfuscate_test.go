package difyapp

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestTemplateAsset(t *testing.T) {
	tpl := TemplateFor("sillytavern-main-v1")
	if tpl == nil {
		t.Fatal("template missing for sillytavern-main-v1")
	}
	vars, err := ExtractStartVariables(tpl.Asset)
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 403 {
		t.Fatalf("canonical vars = %d, want 403", len(vars))
	}
	if vars[0] != "system_prompt" || vars[len(vars)-1] != "assistant_prefill" {
		t.Fatalf("unexpected var layout: first=%q last=%q", vars[0], vars[len(vars)-1])
	}
	if TemplateFor("general") != nil {
		t.Fatal("general must not have a downloadable template")
	}
}

func TestApplyModelConfig(t *testing.T) {
	tpl := TemplateFor("sillytavern-main-v1")
	configured, err := ApplyModelConfig(tpl.Asset, ModelOptions{
		ModelKey:         "claude-opus-4-6",
		Provider:         "anthropic",
		DependencyPlugin: "langgenius/anthropic",
		DependencyVer:    "0.3.26",
		DependencyHash:   "abc123",
		ParamsJSON:       `{"context_1m":true,"max_tokens":8192,"temperature":0.9}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(configured, &doc); err != nil {
		t.Fatal(err)
	}
	root := doc.Content[0]
	dependencies, ok := seqChild(root, "dependencies")
	if !ok || len(dependencies.Content) == 0 {
		t.Fatal("dependencies missing")
	}
	value, _ := mapChild(dependencies.Content[0], "value")
	pin, _ := mapScalar(value, "marketplace_plugin_unique_identifier")
	if pin != "langgenius/anthropic:0.3.26@abc123" {
		t.Fatalf("dependency pin = %q", pin)
	}
	workflow, _ := mapChild(root, "workflow")
	graph, _ := mapChild(workflow, "graph")
	nodes, _ := seqChild(graph, "nodes")
	var model *yaml.Node
	for _, node := range nodes.Content {
		data, _ := mapChild(node, "data")
		if typ, _ := mapScalar(data, "type"); typ == "llm" {
			model, _ = mapChild(data, "model")
			break
		}
	}
	if model == nil {
		t.Fatal("LLM model missing")
	}
	if name, _ := mapScalar(model, "name"); name != "claude-opus-4-6" {
		t.Fatalf("model name = %q", name)
	}
	if provider, _ := mapScalar(model, "provider"); provider != "langgenius/anthropic/anthropic" {
		t.Fatalf("provider = %q", provider)
	}
	params, ok := mapChild(model, "completion_params")
	if !ok || params.Kind != yaml.MappingNode {
		t.Fatal("completion_params missing")
	}
	if maxTokens, _ := mapScalar(params, "max_tokens"); maxTokens != "8192" {
		t.Fatalf("max_tokens = %q", maxTokens)
	}
	if _, ok := mapChild(params, "reasoning_effort"); ok {
		t.Fatal("base model params leaked into replacement params")
	}
	if vars, err := ExtractStartVariables(configured); err != nil || len(vars) != 403 {
		t.Fatalf("configured variables = %d err=%v", len(vars), err)
	}
}

func TestApplyModelConfigRejectsInvalidParams(t *testing.T) {
	_, err := ApplyModelConfig(TemplateFor("sillytavern-main-v1").Asset, ModelOptions{
		ModelKey: "x", Provider: "p", DependencyPlugin: "a/b", DependencyVer: "1", DependencyHash: "h", ParamsJSON: `[]`,
	})
	if err == nil {
		t.Fatal("array params_json should be rejected")
	}
}

func TestGenerateObfuscated(t *testing.T) {
	tpl := TemplateFor("sillytavern-main-v1")
	for i := 0; i < 3; i++ {
		res, err := GenerateObfuscated(tpl.Asset, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Mapping) != 403 {
			t.Fatalf("mapping size = %d, want 403", len(res.Mapping))
		}
		if res.DummyCount < 5 || res.DummyCount > 15 {
			t.Fatalf("dummy count %d outside [5,15]", res.DummyCount)
		}
		vars, err := ExtractStartVariables(res.DSL)
		if err != nil {
			t.Fatalf("obfuscated DSL re-extract: %v", err)
		}
		if len(vars) != 403+res.DummyCount {
			t.Fatalf("obfuscated vars = %d, want %d", len(vars), 403+res.DummyCount)
		}
		// No canonical names anywhere in the output.
		for _, name := range []string{"system_prompt", "user_0", "assistant_200", "assistant_prefill", "sillytavern"} {
			if strings.Contains(string(res.DSL), name) {
				t.Fatalf("canonical leak %q in generation %d", name, i)
			}
		}
		// Mapping round-trips: every canonical maps to one of the start vars.
		seen := map[string]bool{}
		for _, v := range vars {
			seen[v] = true
		}
		for canonical, obf := range res.Mapping {
			if !seen[obf] {
				t.Fatalf("mapping %s -> %s not present in DSL vars", canonical, obf)
			}
		}
		// Dummy variable nodes must be importable by Dify: options must be an
		// actual sequence (renders as []) and required must be a real bool
		// key/value pair, not a misplaced !!bool scalar key.
		var doc yaml.Node
		if err := yaml.Unmarshal(res.DSL, &doc); err != nil {
			t.Fatalf("obfuscated DSL does not parse: %v", err)
		}
		// Walk start-node variables and verify every dummy is importable.
		workflow, _ := mapChild(doc.Content[0], "workflow")
		graph, _ := mapChild(workflow, "graph")
		nodes, _ := seqChild(graph, "nodes")
		checkedDummy := 0
		for _, node := range nodes.Content {
			data, ok := mapChild(node, "data")
			if !ok {
				continue
			}
			if typ, _ := mapScalar(data, "type"); typ != "start" {
				continue
			}
			varsSeq, _ := seqChild(data, "variables")
			for _, v := range varsSeq.Content {
				name, _ := mapScalar(v, "variable")
				if !strings.HasPrefix(name, "x_") {
					continue
				}
				checkedDummy++
				options, ok := mapChild(v, "options")
				if !ok || options.Kind != yaml.SequenceNode {
					t.Fatalf("dummy %q: options is %v, want sequence", name, options)
				}
				required, ok := mapChild(v, "required")
				if !ok || required.Kind != yaml.ScalarNode || required.Tag != "!!bool" {
					t.Fatalf("dummy %q: required is %v/%v, want !!bool scalar", name, required.Kind, required.Tag)
				}
			}
		}
		if checkedDummy != res.DummyCount {
			t.Fatalf("checked dummy vars = %d, want %d", checkedDummy, res.DummyCount)
		}
		// Distinct generations differ.
		if i > 0 {
			prev, _ := GenerateObfuscated(tpl.Asset, nil)
			if string(prev.DSL) == string(res.DSL) {
				t.Fatal("two generations produced identical DSL")
			}
		}
	}
}

func TestObfuscatedDeterministicPerSeed(t *testing.T) {
	tpl := TemplateFor("sillytavern-main-v1")
	seedA := []byte("seed-a")
	seedB := []byte("seed-b")
	a, err := GenerateObfuscated(tpl.Asset, seedA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateObfuscated(tpl.Asset, seedB)
	if err != nil {
		t.Fatal(err)
	}
	if string(a.DSL) == string(b.DSL) {
		t.Fatal("different seeds produced identical DSL")
	}
	// Same seed reproduces the same mapping (keys derive from the seed).
	a2, err := GenerateObfuscated(tpl.Asset, seedA)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range a.Mapping {
		if a2.Mapping[k] != v {
			t.Fatalf("same seed produced different mapping for %s", k)
		}
	}
}
