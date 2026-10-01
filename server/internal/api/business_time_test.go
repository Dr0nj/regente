package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestI06SettingsRejectInvalidTime(t *testing.T) {
	srv, done := newTestServer(t)
	defer done()
	for _, body := range []string{`{"daily_timezone":"Local","env_label":"must-not-write"}`, `{"daily_timezone":"Not/AZone"}`, `{"daily_at":"24:00"}`, `{"daily_at":"6:00"}`} {
		resp, err := srv.Client().Do(authReq(t, http.MethodPut, srv.URL+"/api/settings", body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("%s: status %d", body, resp.StatusCode)
		}
	}
	resp, err := srv.Client().Do(authReq(t, http.MethodPut, srv.URL+"/api/settings", `{"daily_timezone":"America/Sao_Paulo","daily_at":"06:00"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != 200 || got["daily_timezone"] != "America/Sao_Paulo" || got["daily_at"] != "06:00" || got["env_label"] != "" {
		t.Fatalf("settings %d %v", resp.StatusCode, got)
	}
}
