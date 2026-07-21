package translator

import (
	"strings"
	"testing"

	"dify2api/openai"
)

func msgs(list ...[2]string) []openai.Message {
	var out []openai.Message
	for _, m := range list {
		out = append(out, openai.Message{Role: m[0], Content: openai.MessageContent(m[1])})
	}
	return out
}

func TestTranslateForService_General(t *testing.T) {
	inputs, _, err := TranslateForService("general", msgs([2]string{"user", "hi"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["user_0"] != "hi" || len(inputs) != 1 {
		t.Errorf("inputs = %v", inputs)
	}

	cases := [][]openai.Message{
		msgs([2]string{"system", "s"}, [2]string{"user", "u"}), // system not allowed
		msgs([2]string{"user", "a"}, [2]string{"user", "b"}),   // multiple
		msgs([2]string{"assistant", "a"}),                      // wrong role
	}
	for i, c := range cases {
		if _, _, err := TranslateForService("general", c); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestTranslateForService_Custom(t *testing.T) {
	// user only
	inputs, _, err := TranslateForService("custom", msgs([2]string{"user", "u"}))
	if err != nil || inputs["user_0"] != "u" || inputs["system_prompt"] != "" {
		t.Errorf("user only: %v, err %v", inputs, err)
	}
	// system + user
	inputs, _, err = TranslateForService("custom", msgs([2]string{"system", "s"}, [2]string{"user", "u"}))
	if err != nil || inputs["system_prompt"] != "s" || inputs["user_0"] != "u" {
		t.Errorf("system+user: %v, err %v", inputs, err)
	}
	// violations
	if _, _, err := TranslateForService("custom", msgs([2]string{"user", "u"}, [2]string{"system", "s"})); err == nil {
		t.Error("reversed order should fail")
	}
	if _, _, err := TranslateForService("custom", msgs([2]string{"assistant", "a"})); err == nil {
		t.Error("assistant single should fail")
	}
}

func TestTranslateForService_WebsiteSummary(t *testing.T) {
	// url only
	inputs, _, err := TranslateForService("website-summary", msgs([2]string{"user", "https://example.com"}))
	if err != nil || inputs["request_url"] != "https://example.com" || inputs["request_instruction"] != "" {
		t.Errorf("url only: %v, err %v", inputs, err)
	}
	// instruction + url
	inputs, _, err = TranslateForService("website-summary", msgs([2]string{"system", "要点"}, [2]string{"user", "http://a.b/c"}))
	if err != nil || inputs["request_instruction"] != "要点" || inputs["request_url"] != "http://a.b/c" {
		t.Errorf("full: %v, err %v", inputs, err)
	}
	// bad url
	if _, _, err := TranslateForService("website-summary", msgs([2]string{"user", "not-a-url"})); err == nil {
		t.Error("non-url should fail")
	} else if !strings.Contains(err.Error(), "request_url") {
		t.Errorf("error should mention request_url: %v", err)
	}
	// reversed order
	if _, _, err := TranslateForService("website-summary", msgs([2]string{"user", "https://a"}, [2]string{"system", "s"})); err == nil {
		t.Error("reversed order should fail")
	}
}

func TestTranslateForService_SillyTavern(t *testing.T) {
	inputs, _, err := TranslateForService("sillytavern-main-trimmed", msgs(
		[2]string{"system", "S"}, [2]string{"user", "U0"}, [2]string{"assistant", "A1"}, [2]string{"user", "U1"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["system_prompt"] != "S" || inputs["assistant_1"] != "A1" || inputs["user_1"] != "U1" {
		t.Errorf("inputs = %v", inputs)
	}
}

func TestTranslateForService_UnknownRejected(t *testing.T) {
	_, _, err := TranslateForService("general-preview", msgs([2]string{"user", "u"}))
	if err == nil {
		t.Fatal("deprecated/unknown service should be rejected (strict mode)")
	}
	if !strings.Contains(err.Error(), "unsupported service") {
		t.Errorf("error = %v", err)
	}
}

func TestTranslateForService_ImageProcessing(t *testing.T) {
	img := openai.Message{
		Role:    "user",
		Content: openai.MessageContent("describe this"),
		Images:  []string{"https://example.com/a.png", "data:image/png;base64,QUJD"},
	}
	inputs, images, err := TranslateForService("image-processing", []openai.Message{img})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inputs["user_request"] != "describe this" {
		t.Errorf("user_request = %q", inputs["user_request"])
	}
	if len(images) != 2 || images[0] != "https://example.com/a.png" {
		t.Errorf("images = %v", images)
	}

	// No image -> error.
	if _, _, err := TranslateForService("image-processing", msgs([2]string{"user", "describe"})); err == nil {
		t.Error("missing image should fail")
	}
	// Empty instruction -> error even with image.
	imgEmpty := openai.Message{Role: "user", Content: openai.MessageContent("  "), Images: []string{"https://a/b.png"}}
	if _, _, err := TranslateForService("image-processing", []openai.Message{imgEmpty}); err == nil {
		t.Error("empty user_request should fail")
	}
}

func TestTranslateForService_ShujukuFilling(t *testing.T) {
	// Full layout: system + user_0 + A U A U A U A(prefill)
	inputs, _, err := TranslateForService("sillytavern-SP·数据库-填表", msgs(
		[2]string{"system", "S"},
		[2]string{"user", "U0"},
		[2]string{"assistant", "A0"},
		[2]string{"user", "U1"},
		[2]string{"assistant", "A1"},
		[2]string{"user", "U2"},
		[2]string{"assistant", "A2"},
		[2]string{"user", "U3"},
		[2]string{"assistant", "P"},
	))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := map[string]string{
		"system_prompt": "S", "user_0": "U0",
		"assistant_0": "A0", "user_1": "U1", "assistant_1": "A1",
		"user_2": "U2", "assistant_2": "A2", "user_3": "U3", "assistant_prefill": "P",
	}
	for k, v := range want {
		if inputs[k] != v {
			t.Errorf("inputs[%q] = %q, want %q", k, inputs[k], v)
		}
	}

	// Without user_0 (full 8 messages).
	inputs, _, err = TranslateForService("sillytavern-SP·数据库-填表", msgs(
		[2]string{"system", "S"},
		[2]string{"assistant", "A0"},
		[2]string{"user", "U1"},
		[2]string{"assistant", "A1"},
		[2]string{"user", "U2"},
		[2]string{"assistant", "A2"},
		[2]string{"user", "U3"},
		[2]string{"assistant", "P"},
	))
	if err != nil {
		t.Fatalf("without user_0: unexpected error: %v", err)
	}
	if inputs["system_prompt"] != "S" || inputs["user_0"] != "" || inputs["assistant_0"] != "A0" || inputs["assistant_prefill"] != "P" {
		t.Errorf("without user_0: %v", inputs)
	}

	// Violations.
	bad := [][]openai.Message{
		msgs([2]string{"user", "U"}),                                                     // no system
		msgs([2]string{"system", "S"}),                                                   // system only
		msgs([2]string{"assistant", "A"}),                                                // no system
		msgs([2]string{"system", "S"}, [2]string{"user", "U"}),                        // user_0 without rest
		msgs([2]string{"system", "S"}, [2]string{"assistant", "A"}),                   // too few alternation slots
		msgs([2]string{"system", "S"}, [2]string{"assistant", "A"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}), // wrong alternation
		msgs([2]string{"system", "S"}, [2]string{"assistant", " "}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "P"}), // empty content
		msgs([2]string{"system", " "}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "P"}), // empty system
		msgs([2]string{"system", "S"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "A"}, [2]string{"user", "U"}, [2]string{"assistant", "P"}, [2]string{"user", "X"}), // too many
	}
	for i, c := range bad {
		if _, _, err := TranslateForService("sillytavern-SP·数据库-填表", c); err == nil {
			t.Errorf("bad case %d should fail", i)
		}
	}
}

func TestServiceOfModel(t *testing.T) {
	cases := map[string]string{
		"[general]claude-opus-4-6": "general",
		"[website-summary]gpt-5.5": "website-summary",
		"plain-model":              "",
		"[]empty":                  "",
	}
	for model, want := range cases {
		if got := ServiceOfModel(model); got != want {
			t.Errorf("ServiceOfModel(%q) = %q, want %q", model, got, want)
		}
	}
}
