package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ resource.Resource = &ipAccessListResource{}

type ipAccessListResource struct {
	client *Client
}

type ipAccessListModel struct {
	IPRanges types.List `tfsdk:"ip_ranges"`
}

func NewIPAccessListResource() resource.Resource {
	return &ipAccessListResource{}
}

func (r *ipAccessListResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ip_access_list"
}

func (r *ipAccessListResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the enterprise IP access list. This is a singleton: only one should be declared per " +
			"enterprise, and applying it replaces the entire list. Destroying the resource clears the list.",
		Attributes: map[string]schema.Attribute{
			"ip_ranges": schema.ListAttribute{
				Description: "CIDR ranges allowed to access the enterprise. The API normalizes each entry, so use canonical CIDR notation (e.g. \"203.0.113.0/24\").",
				ElementType: types.StringType,
				Required:    true,
			},
		},
	}
}

func (r *ipAccessListResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *ipAccessListResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ipAccessListModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ranges := listToStrings(ctx, plan.IPRanges, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.IPAccessListResponse
	if err := r.client.Put(ctx, enterpriseIPAccessListPath, api.IPAccessListReplaceRequest{IPRanges: ranges}, &result); err != nil {
		resp.Diagnostics.AddError("Failed to set IP access list", err.Error())
		return
	}

	resp.Diagnostics.Append(mapIPAccessListToModel(ctx, result.IPRanges, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ipAccessListResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ipAccessListModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.IPAccessListResponse
	err := r.client.Get(ctx, enterpriseIPAccessListPath, &result)
	if IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read IP access list", err.Error())
		return
	}

	resp.Diagnostics.Append(mapIPAccessListToModel(ctx, result.IPRanges, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ipAccessListResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ipAccessListModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ranges := listToStrings(ctx, plan.IPRanges, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	var result api.IPAccessListResponse
	if err := r.client.Put(ctx, enterpriseIPAccessListPath, api.IPAccessListReplaceRequest{IPRanges: ranges}, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update IP access list", err.Error())
		return
	}

	resp.Diagnostics.Append(mapIPAccessListToModel(ctx, result.IPRanges, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ipAccessListResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.client.Delete(ctx, enterpriseIPAccessListPath, nil); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to clear IP access list", err.Error())
	}
}

func mapIPAccessListToModel(ctx context.Context, ranges []string, model *ipAccessListModel) diag.Diagnostics {
	list, diags := types.ListValueFrom(ctx, types.StringType, ranges)
	model.IPRanges = list
	return diags
}
