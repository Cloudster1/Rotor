//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// api calls Rotor's api as the signed-in browser, with a JSON body when one is given.
func (b *browser) api(method, path string, body any) (int, map[string]any) {
	b.t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, _ := json.Marshal(body)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, b.app+"/api/v1"+path, reader)
	if err != nil {
		b.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, text := b.do(req)
	var out map[string]any
	_ = json.Unmarshal([]byte(text), &out)
	return resp.StatusCode, out
}
