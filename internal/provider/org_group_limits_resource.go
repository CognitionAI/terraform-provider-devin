package provider

import (
	"context"
	"fmt"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &orgGroupLimitsResource{}

type orgGroupLimitsResource struct {
	client *Client
}

type orgGroupModel struct {
	Name         types.String `tfsdk:"name"`
	OrgIDs       types.Set    `tfsdk:"org_ids"`
	MaxCycleACUs types.Int64  `tfsdk:"max_cycle_acus"`
}

type orgGroupLimitsModel struct {
	Groups []orgGroupModel `tfsdk:"groups"`
}

func NewOrgGroupLimitsResource() resource.Resource {
	return &orgGroupLimitsResource{}
}

func (r *orgGroupLimitsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_group_limits"
}

func (r *orgGroupLimitsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the enterprise's org-group ACU limit configuration. This is a singleton: only one should be " +
			"declared per enterprise, and applying it replaces the entire set of groups. Each org may belong to only one group.",
		Attributes: map[string]schema.Attribute{
			"groups": schema.SetNestedAttribute{
				Description: "Org groups and their limits. Replacing this set replaces the whole configuration.",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Description: "Group name.",
							Required:    true,
						},
						"org_ids": schema.SetAttribute{
							Description: "Organization IDs in this group. Each org may appear in only one group.",
							ElementType: types.StringType,
							Required:    true,
						},
						"max_cycle_acus": schema.Int64Attribute{
							Description: "Maximum ACUs per cycle shared across the group. Omit for no limit.",
							Optional:    true,
						},
					},
				},
			},
		},
	}
}

func (r *orgGroupLimitsResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *orgGroupLimitsResource) put(ctx context.Context, plan *orgGroupLimitsModel, diags *diag.Diagnostics) {
	body := api.OrgGroupsConfig{Groups: map[string]api.OrgGroupConfig{}}
	for _, group := range plan.Groups {
		name := group.Name.ValueString()
		if _, exists := body.Groups[name]; exists {
			diags.AddError("Duplicate group name", fmt.Sprintf("Group name %q appears more than once.", name))
			return
		}
		var orgIDs []string
		diags.Append(group.OrgIDs.ElementsAs(ctx, &orgIDs, false)...)
		cfg := api.OrgGroupConfig{OrgIds: orgIDs}
		if !group.MaxCycleACUs.IsNull() && !group.MaxCycleACUs.IsUnknown() {
			cfg.MaxCycleAcus = nullable.NewNullableWithValue(int(group.MaxCycleACUs.ValueInt64()))
		}
		body.Groups[name] = cfg
	}
	if diags.HasError() {
		return
	}

	var result api.OrgGroupsConfig
	if err := r.client.Put(ctx, enterpriseOrgGroupLimitsPath, body, &result); err != nil {
		diags.AddError("Failed to set org group limits", err.Error())
		return
	}
	diags.Append(mapOrgGroupsToModel(ctx, result, plan)...)
}

func (r *orgGroupLimitsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgGroupLimitsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.put(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgGroupLimitsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgGroupLimitsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.OrgGroupsConfig
	err := r.client.Get(ctx, enterpriseOrgGroupLimitsPath, &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read org group limits", err.Error())
		return
	}

	resp.Diagnostics.Append(mapOrgGroupsToModel(ctx, result, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *orgGroupLimitsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan orgGroupLimitsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.put(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *orgGroupLimitsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	// Clearing the config means an empty group set.
	body := api.OrgGroupsConfig{Groups: map[string]api.OrgGroupConfig{}}
	var result api.OrgGroupsConfig
	if err := r.client.Put(ctx, enterpriseOrgGroupLimitsPath, body, &result); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to clear org group limits", err.Error())
	}
}

func mapOrgGroupsToModel(ctx context.Context, config api.OrgGroupsConfig, model *orgGroupLimitsModel) diag.Diagnostics {
	var diags diag.Diagnostics
	groups := make([]orgGroupModel, 0, len(config.Groups))
	for name, cfg := range config.Groups {
		orgIDs, d := types.SetValueFrom(ctx, types.StringType, cfg.OrgIds)
		diags = append(diags, d...)
		groups = append(groups, orgGroupModel{
			Name:         types.StringValue(name),
			OrgIDs:       orgIDs,
			MaxCycleACUs: int64FromNullable(cfg.MaxCycleAcus),
		})
	}
	model.Groups = groups
	return diags
}
