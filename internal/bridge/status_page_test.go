// SPDX-FileCopyrightText: 2026 Sofie Finnes <sofie+git@finnes.dev>
// SPDX-License-Identifier: MPL-2.0
package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPublicReadFailsClosed(t *testing.T) {
	for _, body := range []string{`{}`, `{"config":{},"publicGroupList":null}`, `not-json`, `{"publicGroupList":[{"monitorList":[{"sendUrl":"yes"}]}]}`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer s.Close()
			if _, err := readPage(t.Context(), s.URL, "page"); err == nil {
				t.Fatal("incomplete or malformed response accepted as current state")
			}
		})
	}
}
func TestKumaVisibilityEncoding(t *testing.T) {
	var page pageSnapshot
	if err := json.Unmarshal([]byte(`{"config":{"published":true},"publicGroupList":[{"id":1,"monitorList":[{"id":2,"sendUrl":1},{"id":3,"sendUrl":0},{"id":4,"sendUrl":true}]}]}`), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Groups[0].Monitors[0].SendURL || page.Groups[0].Monitors[1].SendURL || !page.Groups[0].Monitors[2].SendURL {
		t.Fatal("Kuma visibility flags were decoded incorrectly")
	}
}
