package provider

import (
	"context"
	"net/url"

	"github.com/cognitionai/terraform-provider-devin/internal/api"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &rolesDataSource{}

type rolesDataSource struct {
	client *Client
}

type roleModel struct {
	RoleID   types.String `tfsdk:"role_id"`
	RoleName types.String `tfsdk:"role_name"`
	RoleType types.String `tfsdk:"role_type"`
}

type rolesDataSourceModel struct {
	Roles []roleModel `tfsdk:"roles"`
}

func NewRolesDataSource() datasource.DataSource {
	return &rolesDataSource{}
}

func (d *rolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists the roles available in the enterprise, e.g. to look up role IDs when assigning members or service users.",
		Attributes: map[string]schema.Attribute{
			"roles": schema.ListNestedAttribute{
				Description: "Roles available to the enterprise.",
				Computed:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
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
			},
		},
	}
}

func (d *rolesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d.client = req.ProviderData.(*Client)
}

func (d *rolesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state rolesDataSourceModel
	state.Roles = []roleModel{}

	cursor := ""
	for {
		query := url.Values{}
		query.Set("first", "100")
		if cursor != "" {
			query.Set("after", cursor)
		}

		var page api.PaginatedResponseRole
		if err := d.client.Get(ctx, enterpriseRolesPath+"?"+query.Encode(), &page); err != nil {
			resp.Diagnostics.AddError("Failed to list roles", err.Error())
			return
		}

		for _, item := range page.Items {
			state.Roles = append(state.Roles, roleModel{
				RoleID:   types.StringValue(item.RoleID),
				RoleName: types.StringValue(item.RoleName),
				RoleType: types.StringValue(string(item.RoleType)),
			})
		}

		if page.HasNextPage == nil || !*page.HasNextPage || !page.EndCursor.IsSpecified() || page.EndCursor.IsNull() {
			break
		}
		cursor = page.EndCursor.MustGet()
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
