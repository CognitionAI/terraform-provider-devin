package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestOncallResponderModifyPlanRequiresIssueTypeOnCreate(t *testing.T) {
	ctx := context.Background()
	r := &oncallResponderResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	objectType, ok := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	if !ok {
		t.Fatal("schema is not an object")
	}

	alerts := "alerts"
	responder := func(orgID string, issueType *string) tftypes.Value {
		values := map[string]tftypes.Value{}
		for name, attrType := range objectType.AttributeTypes {
			values[name] = tftypes.NewValue(attrType, nil)
		}
		values["org_id"] = tftypes.NewValue(tftypes.String, orgID)
		values["triage_issue_type"] = tftypes.NewValue(tftypes.String, issueType)
		return tftypes.NewValue(objectType, values)
	}
	none := tftypes.NewValue(objectType, nil)

	tests := []struct {
		name      string
		state     tftypes.Value
		plan      tftypes.Value
		wantError bool
	}{
		{"create without type", none, responder("org-1", nil), true},
		{"create with type", none, responder("org-1", &alerts), false},
		{"legacy edited in place", responder("org-1", nil), responder("org-1", nil), false},
		{"legacy replaced by org change without type", responder("org-1", nil), responder("org-2", nil), true},
		{"legacy replaced by setting type", responder("org-1", nil), responder("org-1", &alerts), false},
		{"type removed", responder("org-1", &alerts), responder("org-1", nil), true},
		{"destroy", responder("org-1", nil), none, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := resource.ModifyPlanResponse{}
			r.ModifyPlan(ctx, resource.ModifyPlanRequest{
				State: tfsdk.State{Schema: schemaResp.Schema, Raw: tt.state},
				Plan:  tfsdk.Plan{Schema: schemaResp.Schema, Raw: tt.plan},
			}, &resp)
			if got := resp.Diagnostics.HasError(); got != tt.wantError {
				t.Fatalf("HasError() = %v, want %v: %v", got, tt.wantError, resp.Diagnostics)
			}
		})
	}
}
