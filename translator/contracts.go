package translator

import (
	"fmt"
	"strings"

	"dify2api/openai"
)

// ServiceInfo describes a supported service for the registry (shown in the
// dashboard dropdown).
type ServiceInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// Deprecated marks services scheduled for removal in v2 (still fully
	// supported during v1.x; surfaced as badges in both sites).
	Deprecated bool `json:"deprecated"`
	// Downloadable marks services whose Dify App template can be generated
	// by the authenticated download endpoints. It keeps the SPA capability-
	// driven instead of hard-coding a specific service name.
	Downloadable bool `json:"downloadable"`
}

// serviceRegistry is the ordered list of supported services.
//
// DEV EXTENSION POINT: to support a new service, (1) add an entry here,
// (2) add its contract case in TranslateForService and ContractVarsFor,
// (3) create the corresponding Dify App with matching input variables.
var serviceRegistry = []ServiceInfo{
	{Name: "general", Label: "通用单轮问答（仅 user_0）"},
	{Name: "custom", Label: "自定义单轮问答（user_0 + 可选 system_prompt）"},
	{Name: "website-summary", Label: "网页总结（request_url + 可选 request_instruction）"},
	{Name: "image-processing", Label: "图片理解（system_prompt 可选 + user_request + 图片）"},
	{Name: "sillytavern-main-trimmed", Label: "SillyTavern 主对话（1–22 条宽松布局）", Deprecated: true},
	{Name: "sillytavern-main-200", Label: "SillyTavern 主对话（1–403 条宽松布局，200对）", Deprecated: true},
	{Name: "sillytavern-main-v1", Label: "SillyTavern 主对话 v1（混淆模板，200对）", Downloadable: true},
	{Name: "sillytavern-SP·数据库-填表", Label: "SillyTavern 数据库·填表（system + 可选 user_0 + assistant 打头交替 + 预填充）"},
}

// SupportedServices returns the registered services (dashboard dropdown).
func SupportedServices() []ServiceInfo {
	out := make([]ServiceInfo, len(serviceRegistry))
	copy(out, serviceRegistry)
	return out
}

// IsSupportedService reports whether name is a registered service.
func IsSupportedService(name string) bool {
	for _, s := range serviceRegistry {
		if s.Name == name {
			return true
		}
	}
	return false
}

// Service contracts (V1.0.0): each service defines its own message layout and
// the Dify App variables the messages map to.
//
//	general:          exactly one user message            -> user_0
//	custom:           user required, system optional      -> user_0, system_prompt
//	website-summary:  user -> request_url (required),
//	                  system -> request_instruction (optional)
//	image-processing: user -> user_request (required text),
//	                  system -> system_prompt (optional),
//	                  image parts -> input_image_list (>=1)
//	sillytavern-main-trimmed: system (optional) + 0-10 pairs of assistant/user
//	                  -> 22 slots (1-22 messages)
//	                  (database fill-table: system required, user_0 optional,
//	                  then assistant/user alternation with prefill)
//
// Strict mode: unknown services are rejected (configs are gated by the
// registry at bind time).
//
// TranslateForService returns the text inputs, the image references extracted
// from multimodal content parts (data URIs or http(s) URLs), and an error.
func TranslateForService(service string, messages []openai.Message) (map[string]string, []string, error) {
	switch service {
	case "general":
		in, err := translateGeneral(messages)
		return in, nil, err
	case "custom":
		in, err := translateCustom(messages)
		return in, nil, err
	case "website-summary":
		in, err := translateWebsiteSummary(messages)
		return in, nil, err
	case "image-processing":
		return translateImageProcessing(messages)
	case "sillytavern-main-trimmed":
		in, err := TranslateToSlots(messages)
		return in, nil, err
	case "sillytavern-main-200":
		in, err := TranslateToSlots200(messages)
		return in, nil, err
	case "sillytavern-main-v1":
		// Identical contract to sillytavern-main-200 (system + 200 pairs /
		// 403 slots). The gateway remaps canonical keys to the user's
		// obfuscated keys (from the latest template generation) before
		// forwarding; dummy variables are never filled.
		in, err := TranslateToSlots200(messages)
		return in, nil, err
	case "sillytavern-SP·数据库-填表":
		in, err := translateShujukuFilling(messages)
		return in, nil, err
	default:
		return nil, nil, fmt.Errorf("unsupported service %q", service)
	}
}

// translateShujukuFilling maps the SillyTavern database fill-table layout:
//
//	system, [user_0?], assistant_0, user_1, assistant_1, user_2, assistant_2, user_3, assistant_prefill
//
// system_prompt is required; user_0 is optional (the prompt template inserts
// it between system and assistant_0).  All other variables are required, so
// the full alternation (7 slots) must always be provided.  The minimum valid
// input is 8 messages without user_0, or 9 with user_0; the maximum is 9.
func translateShujukuFilling(messages []openai.Message) (map[string]string, error) {
	inputs := map[string]string{
		"system_prompt": "", "user_0": "",
		"assistant_0": "", "user_1": "", "assistant_1": "",
		"user_2": "", "assistant_2": "", "user_3": "", "assistant_prefill": "",
	}

	if len(messages) == 0 || messages[0].Role != "system" {
		return nil, fmt.Errorf("service \"sillytavern-SP·数据库-填表\" expects a system message first")
	}
	inputs["system_prompt"] = strings.TrimSpace(string(messages[0].Content))
	if inputs["system_prompt"] == "" {
		return nil, fmt.Errorf("messages[0] (system_prompt) must not be empty")
	}

	rest := messages[1:]
	if len(rest) == 0 {
		return nil, fmt.Errorf("service \"sillytavern-SP·数据库-填表\" expects at least one message after system (assistant-first or user+assistant)")
	}

	// user_0 is optional: detect whether it is present by checking the role
	// of the first message after system.
	hasUser0 := rest[0].Role == "user"

	if hasUser0 {
		inputs["user_0"] = strings.TrimSpace(string(rest[0].Content))
		rest = rest[1:]
	}

	// The remaining 7 alternation slots are all required by the Dify App.
	slots := []struct{ role, key string }{
		{"assistant", "assistant_0"},
		{"user", "user_1"},
		{"assistant", "assistant_1"},
		{"user", "user_2"},
		{"assistant", "assistant_2"},
		{"user", "user_3"},
		{"assistant", "assistant_prefill"},
	}

	if len(rest) != len(slots) {
		return nil, fmt.Errorf("service \"sillytavern-SP·数据库-填表\" expects exactly %d messages after system%s, got %d",
			len(slots)+boolToInt(hasUser0), user0Suffix(hasUser0), len(messages))
	}

	for j, m := range rest {
		want := slots[j]
		if m.Role != want.role {
			return nil, fmt.Errorf("messages[%d]: expected role %q, got %q (layout: S [U?] A U A U A U A)",
				1+boolToInt(hasUser0)+j, want.role, m.Role)
		}
		content := strings.TrimSpace(string(m.Content))
		if content == "" {
			return nil, fmt.Errorf("messages[%d] (%s) must not be empty", 1+boolToInt(hasUser0)+j, want.key)
		}
		inputs[want.key] = content
	}
	return inputs, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func user0Suffix(has bool) string {
	if has {
		return " (plus user_0)"
	}
	return ""
}

// ServiceOfModel extracts the service prefix from a "[service]backend" model
// id. Returns "" when the model id is not bracketed (custom contract applies).
func ServiceOfModel(model string) string {
	if strings.HasPrefix(model, "[") {
		if idx := strings.Index(model, "]"); idx > 1 {
			return model[1:idx]
		}
	}
	return ""
}

// ContractVars describes the Dify App variables a service sends.
type ContractVars struct {
	Required []string
	Optional []string
}

// ContractVarsFor returns the variable set of a service contract (used to
// validate a Dify App's parameter list when a user binds an App).
// Unknown services report zero vars (the bind gate rejects them anyway).
func ContractVarsFor(service string) ContractVars {
	switch service {
	case "general":
		return ContractVars{Required: []string{"user_0"}}
	case "website-summary":
		return ContractVars{Required: []string{"request_url"}, Optional: []string{"request_instruction"}}
	case "image-processing":
		return ContractVars{Required: []string{"user_request", "input_image_list"}, Optional: []string{"system_prompt"}}
	case "sillytavern-main-trimmed":
		return ContractVars{
			Required: nil,
			Optional: []string{
				"system_prompt", "user_0",
				"assistant_1", "user_1", "assistant_2", "user_2",
				"assistant_3", "user_3", "assistant_4", "user_4",
				"assistant_5", "user_5", "assistant_6", "user_6",
				"assistant_7", "user_7", "assistant_8", "user_8",
				"assistant_9", "user_9", "assistant_10", "user_10",
			},
		}
	case "sillytavern-main-200":
		opts := make([]string, len(slotNames200))
		copy(opts, slotNames200)
		return ContractVars{Required: nil, Optional: opts}
	case "sillytavern-main-v1":
		// Contractually identical to sillytavern-main-200. The obfuscated
		// app's variables are translated back through the user's mapping
		// before this check (dummies are ignored).
		opts := make([]string, len(slotNames200))
		copy(opts, slotNames200)
		return ContractVars{Required: nil, Optional: opts}
	case "sillytavern-SP·数据库-填表":
		return ContractVars{
			Required: []string{"system_prompt", "assistant_0", "user_1", "assistant_1", "user_2", "assistant_2", "user_3", "assistant_prefill"},
			Optional: []string{"user_0"},
		}
	default: // custom
		return ContractVars{Required: []string{"user_0"}, Optional: []string{"system_prompt"}}
	}
}

// AppParamCheck is the verdict of binding a Dify App to a service.
type AppParamCheck struct {
	// Compatible reports whether the App can serve the service: all
	// contract-required variables exist in the App, and no App-REQUIRED
	// variable is left unsent by the contract. Extra App-OPTIONAL variables
	// are allowed (they simply stay unused).
	Compatible bool `json:"compatible"`
	// MissingContractVars: contract-required variables absent from the App (incompatible).
	MissingContractVars []string `json:"missing_contract_vars,omitempty"`
	// UncoveredAppRequired: App-required variables the contract never sends (incompatible).
	UncoveredAppRequired []string `json:"uncovered_app_required,omitempty"`
	// ExtraAppOptional: App-optional variables the contract never sends (allowed, informational).
	ExtraAppOptional []string `json:"extra_app_optional,omitempty"`
}

// CheckAppParams compares a service contract against a Dify App's parameter
// list (appParams: variable name -> required-by-App).
func CheckAppParams(service string, appParams map[string]bool) AppParamCheck {
	cv := ContractVarsFor(service)
	covered := map[string]bool{}
	for _, v := range cv.Required {
		covered[v] = true
	}
	for _, v := range cv.Optional {
		covered[v] = true
	}

	res := AppParamCheck{Compatible: true}
	for _, v := range cv.Required {
		if _, ok := appParams[v]; !ok {
			res.MissingContractVars = append(res.MissingContractVars, v)
			res.Compatible = false
		}
	}
	for name, requiredByApp := range appParams {
		if covered[name] {
			continue
		}
		if requiredByApp {
			res.UncoveredAppRequired = append(res.UncoveredAppRequired, name)
			res.Compatible = false
		} else {
			res.ExtraAppOptional = append(res.ExtraAppOptional, name)
		}
	}
	return res
}

func translateGeneral(messages []openai.Message) (map[string]string, error) {
	if len(messages) != 1 || messages[0].Role != "user" {
		return nil, fmt.Errorf("service \"general\" expects exactly one user message (no system, no history)")
	}
	content := strings.TrimSpace(string(messages[0].Content))
	if content == "" {
		return nil, fmt.Errorf("user message content must not be empty")
	}
	return map[string]string{"user_0": content}, nil
}

func translateCustom(messages []openai.Message) (map[string]string, error) {
	inputs := map[string]string{"system_prompt": "", "user_0": ""}
	switch len(messages) {
	case 1:
		if messages[0].Role != "user" {
			return nil, fmt.Errorf("service \"custom\" expects [user] or [system, user], got a single %q message", messages[0].Role)
		}
		inputs["user_0"] = strings.TrimSpace(string(messages[0].Content))
	case 2:
		if messages[0].Role != "system" || messages[1].Role != "user" {
			return nil, fmt.Errorf("service \"custom\" expects [user] or [system, user]")
		}
		inputs["system_prompt"] = strings.TrimSpace(string(messages[0].Content))
		inputs["user_0"] = strings.TrimSpace(string(messages[1].Content))
	default:
		return nil, fmt.Errorf("service \"custom\" expects [user] or [system, user], got %d messages", len(messages))
	}
	if inputs["user_0"] == "" {
		return nil, fmt.Errorf("user message content must not be empty")
	}
	return inputs, nil
}

func translateWebsiteSummary(messages []openai.Message) (map[string]string, error) {
	inputs := map[string]string{"request_url": "", "request_instruction": ""}
	switch len(messages) {
	case 1:
		if messages[0].Role != "user" {
			return nil, fmt.Errorf("service \"website-summary\" expects [user(url)] or [system(instruction), user(url)]")
		}
		inputs["request_url"] = strings.TrimSpace(string(messages[0].Content))
	case 2:
		if messages[0].Role != "system" || messages[1].Role != "user" {
			return nil, fmt.Errorf("service \"website-summary\" expects [user(url)] or [system(instruction), user(url)]")
		}
		inputs["request_instruction"] = strings.TrimSpace(string(messages[0].Content))
		inputs["request_url"] = strings.TrimSpace(string(messages[1].Content))
	default:
		return nil, fmt.Errorf("service \"website-summary\" expects [user(url)] or [system(instruction), user(url)], got %d messages", len(messages))
	}
	u := inputs["request_url"]
	if u == "" || !(strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")) {
		return nil, fmt.Errorf("request_url must be a non-empty http(s) URL")
	}
	return inputs, nil
}

func translateImageProcessing(messages []openai.Message) (map[string]string, []string, error) {
	inputs := map[string]string{"system_prompt": "", "user_request": ""}
	var images []string
	for _, m := range messages {
		images = append(images, m.Images...)
	}
	switch len(messages) {
	case 1:
		if messages[0].Role != "user" {
			return nil, nil, fmt.Errorf("service \"image-processing\" expects [user] or [system, user]")
		}
		inputs["user_request"] = strings.TrimSpace(string(messages[0].Content))
	case 2:
		if messages[0].Role != "system" || messages[1].Role != "user" {
			return nil, nil, fmt.Errorf("service \"image-processing\" expects [user] or [system, user]")
		}
		inputs["system_prompt"] = strings.TrimSpace(string(messages[0].Content))
		inputs["user_request"] = strings.TrimSpace(string(messages[1].Content))
	default:
		return nil, nil, fmt.Errorf("service \"image-processing\" expects [user] or [system, user], got %d messages", len(messages))
	}
	if inputs["user_request"] == "" {
		return nil, nil, fmt.Errorf("user_request (instruction text) must not be empty")
	}
	if len(images) == 0 {
		return nil, nil, fmt.Errorf("service \"image-processing\" requires at least one image")
	}
	if len(images) > 10 {
		return nil, nil, fmt.Errorf("service \"image-processing\" accepts at most 10 images, got %d", len(images))
	}
	return inputs, images, nil
}
