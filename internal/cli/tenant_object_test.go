package cli

import (
	"encoding/json"
	"testing"
)

func TestBuildTenantPatchObjectField(t *testing.T) {
	schema := parseSchema(json.RawMessage(`{"properties":{"phone_hours":{"type":["object","null"],"x-widget":"weekly_hours"}}}`))
	if schema == nil {
		t.Fatal("schema did not parse")
	}
	patch, err := buildTenantPatch([]string{`phone_hours={"tz":"Europe/Brussels","week":{"mon":[["09:30","12:00"]]}}`}, nil, schema)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(patch)
	if want := `{"phone_hours":{"tz":"Europe/Brussels","week":{"mon":[["09:30","12:00"]]}}}`; string(got) != want {
		t.Fatalf("patch = %s, want %s", got, want)
	}
	if _, err := buildTenantPatch([]string{"phone_hours=ma-vr 9u-12u"}, nil, schema); err == nil {
		t.Fatal("free text accepted for an object field")
	}
}
