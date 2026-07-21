package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockDifyVisionApp mocks /v1/files/upload and /v1/workflows/run for the
// image-processing path.
func mockDifyVisionApp(t *testing.T, uploads *int, captured *map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/files/upload":
			*uploads++
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Errorf("upload should be multipart: %v", err)
			}
			if r.FormValue("user") == "" {
				t.Error("upload missing user field")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"id":"fid-%d"}`, *uploads)
		case "/v1/workflows/run":
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			*captured = body
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"task_id":"t","workflow_run_id":"w","data":{"id":"x","status":"succeeded","outputs":{"text":"IMAGE_DESCRIBED"}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestRouting_ImageProcessing(t *testing.T) {
	var uploads int
	var captured map[string]interface{}
	srv := mockDifyVisionApp(t, &uploads, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[image-processing]claude-sonnet-4-6")

	body := `{"model":"[image-processing]claude-sonnet-4-6","messages":[` +
		`{"role":"system","content":"要点式描述"},` +
		`{"role":"user","content":[` +
		`{"type":"text","text":"这张图里有什么?"},` +
		`{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJD"}},` +
		`{"type":"image_url","image_url":{"url":"https://example.com/x.png"}}` +
		`]}]}`

	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "IMAGE_DESCRIBED") {
		t.Errorf("response should relay workflow text: %s", rec.Body.String())
	}

	// One data URI -> one upload; one URL passthrough.
	if uploads != 1 {
		t.Errorf("uploads = %d, want 1 (only the data URI)", uploads)
	}
	inputs := captured["inputs"].(map[string]interface{})
	if inputs["user_request"] != "这张图里有什么?" || inputs["system_prompt"] != "要点式描述" {
		t.Errorf("text inputs = %v", inputs)
	}
	files, ok := inputs["input_image_list"].([]interface{})
	if !ok || len(files) != 2 {
		t.Fatalf("input_image_list = %v", inputs["input_image_list"])
	}
	f0 := files[0].(map[string]interface{})
	f1 := files[1].(map[string]interface{})
	if f0["transfer_method"] != "local_file" || f0["upload_file_id"] != "fid-1" {
		t.Errorf("file[0] (uploaded) = %v", f0)
	}
	if f1["transfer_method"] != "remote_url" || f1["url"] != "https://example.com/x.png" {
		t.Errorf("file[1] (remote) = %v", f1)
	}
}

func TestRouting_ImageProcessing_NoImage(t *testing.T) {
	var uploads int
	var captured map[string]interface{}
	srv := mockDifyVisionApp(t, &uploads, &captured)
	defer srv.Close()
	gw, key, _ := setupRoutedUser(t, srv.URL, "[image-processing]claude-sonnet-4-6")

	body := `{"model":"[image-processing]claude-sonnet-4-6","messages":[{"role":"user","content":"有什么?"}]}`
	rec := chatRequest(gw, key, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no image); body: %s", rec.Code, rec.Body.String())
	}
	if captured != nil {
		t.Error("request without image must not be forwarded")
	}
}
