package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &usersDataSource{}

type usersDataSource struct {
	client *Client
}

type roleAssignmentModel struct {
	OrgID    types.String `tfsdk:"org_id"`
	RoleID   types.String `tfsdk:"role_id"`
	RoleName types.String `tfsdk:"role_name"`
	RoleType types.String `tfsdk:"role_type"`
}

type userModel struct {
	UserID          types.String          `tfsdk:"user_id"`
	Email           types.String          `tfsdk:"email"`
	Name            types.String          `tfsdk:"name"`
	RoleAssignments []roleAssignmentModel `tfsdk:"role_assignments"`
}

type usersDataSourceModel struct {
	Email types.String `tfsdk:"email"`
	Users []userModel  `tfsdk:"users"`
}

func NewUsersDataSource() datasource.DataSource {
	return &usersDataSource{}
}

func (d *usersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_users"
}

// roleAssignmentsSchema describes a principal's direct role assignments; it is
// shared by the users and service-users data sources.
func roleAssignmentsSchema() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Description: "Direct role assignments. org_id is null for enterprise (account-level) roles.",
		Computed:    true,
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"org_id": schema.StringAttribute{
					Description: "Organization the role is scoped to, or null for an enterprise role.",
					Computed:    true,
				},
				"role_id": schema.StringAttribute{
					Description: "Role ID.",
					Computed:    true,
				},
				"role_name": schema.StringAttribute{
					Description: "Display name of the role.",
					Computed:    true,
				},
				"role_type": schema.StringAttribute{
					Description: "Scope of the role: enterprise or org.",
					Computed:    true,
				},
			},
		},
	}
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the users in the enterprise, e.g. to look up a user_id when assigning organization roles.",
		Attributes: map[string]schema.Attribute{
			"email": schema.StringAttribute{
				Description: "Filter by exact email address (case-insensitive).",
				Optional:    true,
			},
			"users": schema.ListNestedAttribute{
				Description: "Users in the enterprise.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"user_id": schema.StringAttribute{
							Description: "User ID.",
							Computed:    true,
						},
						"email": schema.StringAttribute{
							Description: "Email address of the user.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Display name of the user.",
							Computed:    true,
						},
						"role_assignments": roleAssignmentsSchema(),
					},
				},
			},
		},
	}
}

func (d *usersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *usersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state usersDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Users = []userModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}
		if !state.Email.IsNull() && !state.Email.IsUnknown() {
			query.Set("email", state.Email.ValueString())
		}

		var page api.PaginatedResponseUser
		if err := d.client.Get(ctx, enterpriseMemberUsersPath+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list users", err.Error())
			return
		}

		for _, item := range page.Items {
			state.Users = append(state.Users, userModel{
				UserID:          types.StringValue(item.UserID),
				Email:           stringFromNullable(item.Email),
				Name:            stringFromNullable(item.Name),
				RoleAssignments: mapRoleAssignments(item.RoleAssignments),
			})
		}

		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		cursor = page.EndCursor.MustGet()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func mapRoleAssignments(assignments []api.RoleAssignment) []roleAssignmentModel {
	result := make([]roleAssignmentModel, 0, len(assignments))
	for _, assignment := range assignments {
		result = append(result, roleAssignmentModel{
			OrgID:    stringFromNullable(assignment.OrgID),
			RoleID:   types.StringValue(assignment.Role.RoleID),
			RoleName: types.StringValue(assignment.Role.RoleName),
			RoleType: types.StringValue(string(assignment.Role.RoleType)),
		})
	}
	return result
}
