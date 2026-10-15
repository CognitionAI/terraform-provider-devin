package provider

import (
	"context"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

var _ resource.Resource = &enterpriseBlueprintResource{}
var _ resource.ResourceWithImportState = &enterpriseBlueprintResource{}
var _ resource.ResourceWithIdentity = &enterpriseBlueprintResource{}

type enterpriseBlueprintResource struct {
	client *Client
}

type enterpriseBlueprintModel struct {
	BlueprintID types.String `tfsdk:"blueprint_id"`
	Contents    types.String `tfsdk:"contents"`
}

func NewEnterpriseBlueprintResource() resource.Resource {
	return &enterpriseBlueprintResource{}
}

func (r *enterpriseBlueprintResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enterprise_blueprint"
}

func (r *enterpriseBlueprintResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the enterprise-wide snapshot setup blueprint, which applies to every organization in the " +
			"enterprise. An enterprise has at most one; creating a second fails. Changes don't start a build.",
		Attributes: map[string]schema.Attribute{
			"blueprint_id": schema.StringAttribute{
				Description: "Blueprint ID (assigned by Devin).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"contents": schema.StringAttribute{
				Description: "Blueprint YAML.",
				Required:    true,
			},
		},
	}
}

func (r *enterpriseBlueprintResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = enterpriseBlueprintIdentitySchema()
}

func (r *enterpriseBlueprintResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*Client)
}

func (r *enterpriseBlueprintResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan enterpriseBlueprintModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.CreateEnterpriseBlueprintBody{Contents: nullable.NewNullableWithValue(plan.Contents.ValueString())}
	var result api.Blueprint
	if err := r.client.Post(ctx, enterpriseBlueprintsPath, body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to create enterprise blueprint", err.Error())
		return
	}
	plan.BlueprintID = types.StringValue(result.BlueprintID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, enterpriseBlueprintIdentityModel{BlueprintID: plan.BlueprintID}, &resp.Diagnostics)
}

func (r *enterpriseBlueprintResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state enterpriseBlueprintModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	identity := enterpriseBlueprintIdentityModel{BlueprintID: state.BlueprintID}

	var result api.Blueprint
	err := r.client.Get(ctx, enterpriseBlueprintPath(state.BlueprintID.ValueString()), &result)
	if IsNotFound(err) {
		setIdentity(ctx, resp.Identity, identity, &resp.Diagnostics)
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Failed to read enterprise blueprint", err.Error())
		return
	}

	contents, ok := readBlueprintContents(ctx, r.client, enterpriseBlueprintPath(state.BlueprintID.ValueString()), &resp.Diagnostics)
	if !ok {
		return
	}
	state.Contents = contents
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	setIdentity(ctx, resp.Identity, identity, &resp.Diagnostics)
}

func (r *enterpriseBlueprintResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan enterpriseBlueprintModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := api.UpdateBlueprintContentsBody{Contents: plan.Contents.ValueString()}
	var result api.Blueprint
	if err := r.client.Put(ctx, enterpriseBlueprintPath(plan.BlueprintID.ValueString()), body, &result); err != nil {
		resp.Diagnostics.AddError("Failed to update enterprise blueprint", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	setIdentity(ctx, resp.Identity, enterpriseBlueprintIdentityModel{BlueprintID: plan.BlueprintID}, &resp.Diagnostics)
}

func (r *enterpriseBlueprintResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state enterpriseBlueprintModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Delete(ctx, enterpriseBlueprintPath(state.BlueprintID.ValueString()), nil)
	if err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete enterprise blueprint", err.Error())
	}
}

func (r *enterpriseBlueprintResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importComposite(ctx, req, resp, "blueprint_id")
}
