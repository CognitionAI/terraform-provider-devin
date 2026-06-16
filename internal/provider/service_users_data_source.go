package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &serviceUsersDataSource{}

type serviceUsersDataSource struct {
	client *Client
}

type serviceUserModel struct {
	ServiceUserID   types.String          `tfsdk:"service_user_id"`
	Name            types.String          `tfsdk:"name"`
	ExpiresAt       types.Int64           `tfsdk:"expires_at"`
	RoleAssignments []roleAssignmentModel `tfsdk:"role_assignments"`
}

type serviceUsersDataSourceModel struct {
	ServiceUsers []serviceUserModel `tfsdk:"service_users"`
}

func NewServiceUsersDataSource() datasource.DataSource {
	return &serviceUsersDataSource{}
}

func (d *serviceUsersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_service_users"
}

func (d *serviceUsersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the active service users in the enterprise, e.g. to look up a service_user_id when " +
			"assigning roles.",
		Attributes: map[string]schema.Attribute{
			"service_users": schema.ListNestedAttribute{
				Description: "Active service users in the enterprise.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"service_user_id": schema.StringAttribute{
							Description: "Service user ID.",
							Computed:    true,
						},
						"name": schema.StringAttribute{
							Description: "Display name of the service user.",
							Computed:    true,
						},
						"expires_at": schema.Int64Attribute{
							Description: "Unix timestamp when the service user expires, if set.",
							Computed:    true,
						},
						"role_assignments": roleAssignmentsSchema(),
					},
				},
			},
		},
	}
}

func (d *serviceUsersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *serviceUsersDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state serviceUsersDataSourceModel
	state.ServiceUsers = []serviceUserModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseServiceUser
		if err := d.client.Get(ctx, enterpriseMemberServiceUsersPath+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list service users", err.Error())
			return
		}

		for _, item := range page.Items {
			state.ServiceUsers = append(state.ServiceUsers, serviceUserModel{
				ServiceUserID:   types.StringValue(item.ServiceUserID),
				Name:            types.StringValue(item.Name),
				ExpiresAt:       int64FromNullable(item.ExpiresAt),
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
