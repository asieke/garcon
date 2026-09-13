package setup

import (
	"strings"
	"testing"
)

func TestParsers(t *testing.T) {
	keys := []byte(`[{"name":"anon","api_key":"eyJ...","type":"legacy"},{"name":"default","api_key":"sb_publishable_x","type":"publishable"},{"name":"default","api_key":"sb_secret_y","type":"secret"}]`)
	if secretKey(keys) != "sb_secret_y" {
		t.Errorf("secretKey: %q", secretKey(keys))
	}
	if secretKey([]byte(`[]`)) != "" {
		t.Error("no key must be empty")
	}
	if field([]byte(`{"ref":"abc"}`), "id", "ref") != "abc" || field([]byte(`{"id":"x","ref":"y"}`), "id", "ref") != "x" {
		t.Error("field precedence")
	}
	if org, err := pickOrg([]byte(`[{"id":"one","name":"Only"}]`)); err != nil || org != "one" {
		t.Errorf("single org: %q %v", org, err)
	}
	if _, err := pickOrg([]byte(`[{"id":"a","name":"A"},{"id":"b","name":"B"}]`)); err == nil || !strings.Contains(err.Error(), "--org-id") {
		t.Errorf("several orgs: %v", err)
	}
	if !strings.Contains(Schema, "create table if not exists public.garcon_usage") {
		t.Error("schema not embedded")
	}
	if p := newPassword(); len(p) < 20 || strings.ContainsAny(p, "/+=") {
		t.Errorf("password: %q", p)
	}
}
