package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTeamAndApplicationCreationDates(t *testing.T) {
	f := newMigrationFixture(t)
	team := f.create(t, "/admin/teams", `{"name":"Dated team"}`)
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"Dated app","team_id":%.0f}`, team["id"]))
	if err := f.store.AddTeamMember(context.Background(), int64(team["id"].(float64)), "test-admin", "admin"); err != nil {
		t.Fatal(err)
	}
	status, _, body := f.request(t, http.MethodGet, "/admin/teams/mine", "", "", true)
	var memberships []map[string]any
	if status != http.StatusOK || json.Unmarshal(body, &memberships) != nil ||
		len(memberships) != 1 || memberships[0]["team_created_at"] != team["created_at"] {
		t.Fatalf("membership did not include the team's creation time: %d %s", status, body)
	}
	for _, tc := range []struct {
		created map[string]any
		path    string
	}{
		{team, "/admin/teams"},
		{app, fmt.Sprintf("/admin/applications?team_id=%.0f", team["id"])},
	} {
		createdAt, err := time.Parse(time.RFC3339Nano, tc.created["created_at"].(string))
		if err != nil || createdAt.IsZero() {
			t.Fatalf("invalid creation time: %v, %v", tc.created, err)
		}
		status, _, body := f.request(t, http.MethodGet, tc.path, "", "", true)
		var records []map[string]any
		if status != http.StatusOK || json.Unmarshal(body, &records) != nil {
			t.Fatalf("list %s: %d %s", tc.path, status, body)
		}
		var stored any
		for _, record := range records {
			if record["id"] == tc.created["id"] {
				stored = record["created_at"]
			}
		}
		if stored != tc.created["created_at"] {
			t.Fatalf("create returned %v, list returned %v", tc.created["created_at"], stored)
		}
	}
}

func TestAPIKeyExpirationLifecycle(t *testing.T) {
	f := newMigrationFixture(t)
	team := f.create(t, "/admin/teams", `{"name":"Key team"}`)
	teamID := int64(team["id"].(float64))
	if err := f.store.AddTeamMember(context.Background(), teamID, "test-admin", "admin"); err != nil {
		t.Fatal(err)
	}
	app := f.create(t, "/admin/applications", fmt.Sprintf(`{"name":"Key app","team_id":%d}`, teamID))
	appKeys := fmt.Sprintf("/admin/applications/%.0f/keys", app["id"])
	future := time.Now().UTC().Add(time.Hour).Truncate(time.Millisecond)
	expiry := fmt.Sprintf(`{"name":"Scheduled","expires_at":%q}`, future.Format(time.RFC3339Nano))
	request := func(method, path, payload, token string, admin bool, want int) []byte {
		t.Helper()
		status, _, body := f.request(t, method, path, payload, token, admin)
		if status != want {
			t.Fatalf("%s %s: want %d, got %d %s", method, path, want, status, body)
		}
		return body
	}
	for _, bad := range []string{`"tomorrow"`, `"2000-01-01T00:00:00Z"`, `42`} {
		body := request(http.MethodPost, appKeys, `{"name":"Bad","expires_at":`+bad+`}`, "", true, http.StatusBadRequest)
		if !strings.Contains(string(body), "expires_at") {
			t.Fatalf("missing expiration validation error: %s", body)
		}
	}
	key := f.create(t, appKeys, expiry)
	if key["expires_at"] != future.Format(time.RFC3339Nano) || key["created_at"] == nil {
		t.Fatalf("missing key timestamps: %v", key)
	}
	id := int64(key["id"].(float64))
	keyPath := fmt.Sprintf("/admin/api-keys/%d", id)
	token := key["key"].(string)
	request(http.MethodGet, "/v1/models", "", token, false, http.StatusOK)
	checkKey := func(wantExpiry *time.Time, wantActive bool) {
		t.Helper()
		var keys []struct {
			ID        int64      `json:"id"`
			ExpiresAt *time.Time `json:"expires_at"`
			IsActive  bool       `json:"is_active"`
		}
		body := request(http.MethodGet, appKeys, "", "", true, http.StatusOK)
		if err := json.Unmarshal(body, &keys); err != nil || len(keys) != 1 || keys[0].ID != id {
			t.Fatalf("unexpected key list: %s (%v)", body, err)
		}
		k := keys[0]
		if k.IsActive != wantActive || (wantExpiry == nil) != (k.ExpiresAt == nil) ||
			(wantExpiry != nil && !wantExpiry.Equal(*k.ExpiresAt)) {
			t.Fatalf("unexpected key state: %s", body)
		}
	}
	request(http.MethodPatch, keyPath, `{"name":"Renamed"}`, "", true, http.StatusNoContent)
	checkKey(&future, true)
	request(http.MethodPatch, keyPath, `{"name":"Renamed","expires_at":null}`, "", true, http.StatusNoContent)
	checkKey(nil, true)
	request(http.MethodPatch, keyPath, expiry, "", true, http.StatusNoContent)
	checkKey(&future, true)

	// Move the stored deadline into the past to exercise expired reads without sleeping.
	past := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	if _, err := f.store.DB().Exec(`UPDATE api_keys SET expires_at_ms = ? WHERE id = ?`, past.UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
	checkKey(&past, false)
	request(http.MethodPatch, keyPath, `{"name":"Renamed","expires_at":null}`, "", true, http.StatusConflict)
	request(http.MethodGet, "/v1/models", "", token, false, http.StatusUnauthorized)
	request(http.MethodGet, "/v1/accounting/unused", "", token, false, http.StatusUnauthorized)

	chat := strings.NewReader(`{"model":"openai","messages":[{"role":"user","content":"hi"}]}`)
	req, err := http.NewRequest(http.MethodPost, f.url+"/admin/chat/completions", chat)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Charge-Key-ID", fmt.Sprint(id))
	req.AddCookie(f.cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expired key accepted for charge attribution: %d", resp.StatusCode)
	}

	body := request(http.MethodGet, "/admin/keys/mine", "", "", true, http.StatusOK)
	if string(body) != "[]\n" {
		t.Fatalf("expired key is still selectable: %s", body)
	}
}
