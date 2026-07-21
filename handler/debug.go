package handler

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// debugRequestDump is the on-disk format of request.json in a debug dump folder.
type debugRequestDump struct {
	Time       string `json:"time"`
	RemoteAddr string `json:"remote_addr"`
	Method     string `json:"method"`
	URL        string `json:"url"`
	UserAgent  string `json:"user_agent"`
	Note       string `json:"note"` // "ok", or why dify_inputs.json is absent
	RawBody    string `json:"raw_body"`
}

// dumpDebugRequest saves an intercepted request into a timestamped folder under
// dir: request.json (metadata + raw API request) and, when inputs is non-nil,
// dify_inputs.json (the slot fields prepared for the Dify App).
// Returns the folder path, or "(dump failed)" on error.
func dumpDebugRequest(dir string, r *http.Request, rawBody []byte, note string, inputs map[string]string) string {
	now := time.Now()
	// Nanosecond suffix keeps rapid consecutive requests collision-free even on
	// coarse timers (Windows).
	folder := filepath.Join(dir, fmt.Sprintf("%s_%09d", now.Format("20060102_150405"), now.UnixNano()%1e9))
	if err := os.MkdirAll(folder, 0o755); err != nil {
		log.Printf("[DEBUG] mkdir %s: %v", folder, err)
		return "(dump failed)"
	}

	writeDebugJSON(filepath.Join(folder, "request.json"), debugRequestDump{
		Time:       now.Format(time.RFC3339Nano),
		RemoteAddr: r.RemoteAddr,
		Method:     r.Method,
		URL:        r.URL.String(),
		UserAgent:  r.Header.Get("User-Agent"),
		Note:       note,
		RawBody:    string(rawBody),
	})

	if inputs != nil {
		writeDebugJSON(filepath.Join(folder, "dify_inputs.json"), inputs)
	}

	return folder
}

func writeDebugJSON(path string, v interface{}) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		log.Printf("[DEBUG] marshal %s: %v", path, err)
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		log.Printf("[DEBUG] write %s: %v", path, err)
	}
}
