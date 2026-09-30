package provider

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDecodeAndNullFillJSON(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`{"max_acu_limit":25}`)
	got := decodeAndNullFillJSON[api.AutomationLimits](value, "limits", &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	if value, ok := object["invocations"]; !ok || value != nil {
		t.Fatalf("expected invocations to be explicit null, got %v", object["invocations"])
	}
}

func TestDecodeAndNullFillActionsDoesNotInventReadOnlyMembers(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`[{"type":"start_session","prompt":"x","session":{"platform":"linux"}}]`)
	got := decodeAndNullFillActions(value, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actions []map[string]any
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatal(err)
	}
	session := actions[0]["session"].(map[string]any)
	if _, ok := session["repos"]; ok {
		t.Fatal("null-fill invented read-only repos")
	}
	if _, ok := session["playbook_id"]; ok {
		t.Fatal("null-fill invented read-only playbook_id")
	}
	if _, ok := session["bypass_approval"]; ok {
		t.Fatalf("expected omitted non-nullable bypass_approval to remain omitted, got %v", session["bypass_approval"])
	}
}

func TestDecodeAndNullFillActionsFillsOmittedSessionBlock(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`[{"type":"start_session","prompt":"x"}]`)
	got := decodeAndNullFillActions(value, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var actions []map[string]any
	if err := json.Unmarshal(raw, &actions); err != nil {
		t.Fatal(err)
	}
	session, ok := actions[0]["session"].(map[string]any)
	if !ok {
		t.Fatalf("expected omitted session to be filled with an object, got %v", actions[0]["session"])
	}
	if tags, ok := session["tags"].([]any); !ok || len(tags) != 0 {
		t.Fatalf("expected session tags to be an empty array, got %v", session["tags"])
	}
	for _, name := range []string{"platform", "notifications"} {
		if value, present := session[name]; !present || value != nil {
			t.Fatalf("expected session %s to be explicit null, got %v", name, session[name])
		}
	}
	for _, name := range []string{"bypass_approval", "repos", "playbook_id"} {
		if _, present := session[name]; present {
			t.Fatalf("expected session %s to remain omitted, got %v", name, session[name])
		}
	}
}

func TestDecodeAndNullFillSessionSettingsLeavesUnionValuesOpaque(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`{"net_policy":{"allow":[{"hostname":"example.com"}]}}`)
	got := decodeAndNullFillJSON[api.AutomationSessionSettingsInput](value, "session_settings", &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	netPolicy := object["net_policy"].(map[string]any)
	allow := netPolicy["allow"].([]any)
	if got := allow[0]; got == nil {
		t.Fatal("expected net_policy.allow entry")
	} else if entry := got.(map[string]any); len(entry) != 1 || entry["hostname"] != "example.com" {
		t.Fatalf("expected opaque union entry to be unchanged, got %v", entry)
	}
}

func TestDecodeAndNullFillJSONUsesEmptyListsForNonNullablePointers(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`{}`)
	got := decodeAndNullFillJSON[api.AutomationTools](value, "tools", &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"mcp_servers", "slack_channels", "teams_channels"} {
		value, ok := object[field].([]any)
		if !ok || len(value) != 0 {
			t.Fatalf("expected %s to be an empty array, got %v", field, object[field])
		}
	}
	if value, ok := object["linear_enabled"]; !ok || value != nil {
		t.Fatalf("expected linear_enabled to be explicit null, got %v", object["linear_enabled"])
	}
}

func TestDecodeAndNullFillTriggersClearsOmittedMembers(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`[{"event_type":"schedule:recurring"}]`)
	got := decodeAndNullFillTriggers(value, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var triggers []map[string]any
	if err := json.Unmarshal(raw, &triggers); err != nil {
		t.Fatal(err)
	}
	if conditions, ok := triggers[0]["conditions"]; !ok || conditions != nil {
		t.Fatalf("expected conditions to be explicit null, got %v", triggers[0]["conditions"])
	}
	if replies, ok := triggers[0]["replies"].([]any); !ok || len(replies) != 0 {
		t.Fatalf("expected replies to be an empty array, got %v", triggers[0]["replies"])
	}
}

func TestDecodeAndNullFillJSONLiteralNullClearsGroup(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`null`)
	got := nullableGroupFromJSON[api.AutomationTools](value, "tools", &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if !got.IsNull() {
		t.Fatalf("expected literal JSON null to clear the group, got %v", got)
	}
}

func TestOptionalGroupFromLiteralNullLeavesGroupUnspecified(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`null`)
	got := optionalGroupFrom[api.AutomationLimits](value, "limits", &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got.IsSpecified() {
		t.Fatalf("expected literal JSON null to leave the group unspecified, got %v", got)
	}
}

func TestDecodeAndNullFillTriggersRejectsNullElement(t *testing.T) {
	var diags diag.Diagnostics
	value := jsontypes.NewNormalizedValue(`[null]`)
	_ = decodeAndNullFillTriggers(value, &diags)
	if !diags.HasError() {
		t.Fatal("expected null trigger element to produce a diagnostic")
	}
}

func TestNullFillActionsLeavesUnknownTypesUntouched(t *testing.T) {
	raw := []byte(`[{"type":"triage_session","setup_prompt":"probe"}]`)
	got, _, err := nullFillActions(raw)
	if err != nil {
		t.Fatal(err)
	}
	var wantObject, gotObject []map[string]any
	if err := json.Unmarshal(raw, &wantObject); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &gotObject); err != nil {
		t.Fatal(err)
	}
	if len(gotObject) != len(wantObject) || gotObject[0]["type"] != wantObject[0]["type"] ||
		gotObject[0]["setup_prompt"] != wantObject[0]["setup_prompt"] {
		t.Fatalf("expected unknown action to pass through unchanged, got %s", got)
	}
}

// The executor owns target_devin_id on auto_create message_session actions
// (it binds the session it creates on first fire), so the null-fill must not
// clear it — while an explicitly configured target still passes through, and
// non-auto_create actions still get the member null-filled.
func TestNullFillActionsSkipsExecutorOwnedTarget(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantMember bool
		wantValue  any
	}{
		{"auto_create omits target", `[{"type":"message_session","auto_create":true,"prompt":"p"}]`, false, nil},
		{"auto_create explicit target passes through", `[{"type":"message_session","auto_create":true,"prompt":"p","target_devin_id":"devin-1"}]`, true, "devin-1"},
		{"omitted auto_create omits target", `[{"type":"message_session","prompt":"p"}]`, false, nil},
		{"explicit manual mode still null-filled", `[{"type":"message_session","auto_create":false,"prompt":"p"}]`, true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := nullFillActions([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			var actions []map[string]any
			if err := json.Unmarshal(got, &actions); err != nil {
				t.Fatal(err)
			}
			value, present := actions[0]["target_devin_id"]
			if present != tc.wantMember {
				t.Fatalf("target_devin_id present = %v, want %v (payload %s)", present, tc.wantMember, got)
			}
			if present && value != tc.wantValue {
				t.Fatalf("target_devin_id = %v, want %v", value, tc.wantValue)
			}
		})
	}
}

// Members that are required when creating or adding an action reject an
// explicit null on a merge-semantics update (the API carries the stored
// value only when they are omitted), so the null-fill must leave them
// absent — while explicitly configured values still pass through.
func TestNullFillActionsKeepsRequiredMembersOmitted(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		member    string
		wantSent  bool
		wantValue any
	}{
		{"start_session omitted prompt", `[{"type":"start_session"}]`, "prompt", false, nil},
		{"start_session explicit prompt", `[{"type":"start_session","prompt":"p"}]`, "prompt", true, "p"},
		{"message_session omitted prompt", `[{"type":"message_session","target_devin_id":"devin-1"}]`, "prompt", false, nil},
		{"monitor_session omitted setup_prompt", `[{"type":"monitor_session"}]`, "setup_prompt", false, nil},
		{"monitor_session omitted slack_monitor_config", `[{"type":"monitor_session"}]`, "slack_monitor_config", false, nil},
		{"scan_new_commits omitted scan_id", `[{"type":"scan_new_commits"}]`, "scan_id", false, nil},
		{"scan_new_commits explicit scan_id", `[{"type":"scan_new_commits","scan_id":"scan-1"}]`, "scan_id", true, "scan-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := nullFillActions([]byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			var actions []map[string]any
			if err := json.Unmarshal(got, &actions); err != nil {
				t.Fatal(err)
			}
			value, present := actions[0][tc.member]
			if present != tc.wantSent {
				t.Fatalf("%s present = %v, want %v (payload %s)", tc.member, present, tc.wantSent, got)
			}
			if present && value != tc.wantValue {
				t.Fatalf("%s = %v, want %v", tc.member, value, tc.wantValue)
			}
		})
	}
}

func TestMetadataDeletionHelpers(t *testing.T) {
	value := "v"
	if metadataHasDeletions(map[string]*string{"keep": &value}) {
		t.Error("expected no deletions for a patch of plain values")
	}
	patch := map[string]*string{"keep": &value, "drop": nil}
	if !metadataHasDeletions(patch) {
		t.Error("expected a nil value to count as a deletion")
	}
	replaced := metadataWithoutDeletions(patch)
	if len(replaced) != 1 || replaced["keep"] != &value {
		t.Errorf("expected only the kept key to survive, got %v", replaced)
	}
}

// Every *ActionUpdate model in the generated package must have a null-fill
// dispatch entry, so an action type that enters the published spec (e.g. a
// flag-gated type such as triage_session once it un-gates) cannot silently
// keep the pass-through behavior that skips config-authoritative clearing.
func TestActionUpdateSchemasCoverGeneratedModels(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join("..", "api", "models.gen.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse api models: %v", err)
	}
	pattern := regexp.MustCompile(`^Automation([A-Za-z]+)ActionUpdate$`)
	camelBoundary := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	for _, decl := range file.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if _, ok := ts.Type.(*ast.StructType); !ok {
				continue
			}
			match := pattern.FindStringSubmatch(ts.Name.Name)
			if match == nil {
				continue
			}
			actionType := strings.ToLower(camelBoundary.ReplaceAllString(match[1], "${1}_${2}"))
			if _, ok := actionUpdateSchemas[actionType]; !ok {
				t.Errorf("generated model %s has no actionUpdateSchemas entry for %q; "+
					"add it so its omitted members are null-filled on update",
					ts.Name.Name, actionType)
			}
		}
	}
}

// Unknown action types are reported so updates can warn that the server
// keeps their omitted members.
func TestNullFillActionsReportsSkippedTypes(t *testing.T) {
	_, skipped, err := nullFillActions([]byte(`[{"type":"triage_session","setup_prompt":"p"},{"type":"start_session","prompt":"p"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "triage_session" {
		t.Fatalf("skipped = %v, want [triage_session]", skipped)
	}
}

func TestNormalizedJSONChangedUsesSemanticEquality(t *testing.T) {
	var diags diag.Diagnostics
	plan := jsontypes.NewNormalizedValue(`{"a":1,"b":[true]}`)
	state := jsontypes.NewNormalizedValue(`{ "b": [true], "a": 1 }`)
	if normalizedJSONChanged(context.Background(), plan, state, &diags) {
		t.Fatal("expected semantically equal JSON to be unchanged")
	}
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestStringMapFromPtrPreservesEmptyMap(t *testing.T) {
	var diags diag.Diagnostics
	empty := map[string]string{}

	got := stringMapFromPtr(context.Background(), &empty, &diags)
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if got.IsNull() || got.IsUnknown() {
		t.Fatalf("expected non-null empty map, got %v", got)
	}
	if elements := got.Elements(); len(elements) != 0 {
		t.Fatalf("expected empty map, got %v", elements)
	}

	if got := stringMapFromPtr(context.Background(), nil, &diags); !got.IsNull() {
		t.Fatalf("expected nil map pointer to remain null, got %v", got)
	}
}

func runAsResponse(t *testing.T, raw string) *api.AutomationResponse {
	t.Helper()
	if raw == "" {
		return &api.AutomationResponse{}
	}
	var runAs api.AutomationResponse_RunAs
	if err := runAs.UnmarshalJSON([]byte(raw)); err != nil {
		t.Fatalf("building run_as union from %q: %v", raw, err)
	}
	return &api.AutomationResponse{RunAs: &runAs}
}

func TestAutomationRunAsFromResponse(t *testing.T) {
	cases := []struct {
		name            string
		raw             string
		want            string
		wantServiceUser types.String
	}{
		{"absent", "", "organization", types.StringNull()},
		{"organization", `{"type": "organization"}`, "organization", types.StringNull()},
		{"creator", `{"type": "creator"}`, "creator", types.StringNull()},
		{"service user", `{"type": "service_user", "service_user_id": "service-user-1"}`, "service_user", types.StringValue("service-user-1")},
		{"null", `null`, "organization", types.StringNull()},
		{"empty object", `{}`, "organization", types.StringNull()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, gotServiceUser := automationRunAsFromResponse(runAsResponse(t, tc.raw), types.StringValue(tc.want))
			if got.ValueString() != tc.want || !gotServiceUser.Equal(tc.wantServiceUser) {
				t.Errorf("got (%v, %v), want (%q, %v)", got, gotServiceUser, tc.want, tc.wantServiceUser)
			}
		})
	}
}

// A null prior state must stay null while the server holds the organization
// default (null and "organization" are equivalent, so config-omitted run_as
// must not plan a perpetual reset), while every real out-of-band change
// surfaces as drift.
func TestNormalizeRunAsState(t *testing.T) {
	cases := []struct {
		name   string
		server string
		prior  types.String
		want   types.String
	}{
		{"default with omitted config stays null", "organization", types.StringNull(), types.StringNull()},
		{"default with explicit organization kept", "organization", types.StringValue("organization"), types.StringValue("organization")},
		{"out-of-band switch to creator drifts", "creator", types.StringNull(), types.StringValue("creator")},
		{"creator matches configured creator", "creator", types.StringValue("creator"), types.StringValue("creator")},
		{"out-of-band reset to organization drifts", "organization", types.StringValue("creator"), types.StringValue("organization")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeRunAsState(tc.server, tc.prior); !got.Equal(tc.want) {
				t.Errorf("normalizeRunAsState(%q, %v) = %v, want %v", tc.server, tc.prior, got, tc.want)
			}
		})
	}
}
