package provider

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &DevinProvider{}
var _ provider.ProviderWithListResources = &DevinProvider{}

type DevinProvider struct {
	version string
}

type DevinProviderModel struct {
	APIUrl types.String `tfsdk:"api_url"`
	Token  types.String `tfsdk:"token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &DevinProvider{version: version}
	}
}

func (p *DevinProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "devin"
	resp.Version = p.version
}

func (p *DevinProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage Devin resources via the v3 API. Authenticates with an enterprise or organization service user token.",
		Attributes: map[string]schema.Attribute{
			"api_url": schema.StringAttribute{
				Description: "Devin API base URL. Defaults to DEVIN_API_URL env var, or https://api.devin.ai.",
				Optional:    true,
			},
			"token": schema.StringAttribute{
				Description: "Service user token (cog_ prefix). Defaults to DEVIN_TOKEN env var.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *DevinProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config DevinProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.APIUrl.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Unknown api_url",
			"The provider api_url is derived from a value that is not known until apply. Use a static value or the DEVIN_API_URL environment variable.")
	}
	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Unknown token",
			"The provider token is derived from a value that is not known until apply. Use a static value or the DEVIN_TOKEN environment variable.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiURL := envOrDefault(config.APIUrl, "DEVIN_API_URL", "https://api.devin.ai")
	token := envOrDefault(config.Token, "DEVIN_TOKEN", "")

	if parsed, err := url.Parse(apiURL); err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		resp.Diagnostics.AddAttributeError(path.Root("api_url"), "Invalid api_url",
			fmt.Sprintf("The api_url %q is not a valid http(s) URL.", apiURL))
		return
	}

	if token == "" {
		resp.Diagnostics.AddError("Missing token", "Set the token attribute or DEVIN_TOKEN environment variable.")
		return
	}

	client := &Client{
		BaseURL:            apiURL,
		Token:              token,
		HTTPClient:         NewRetryHTTPClient(),
		MutatingHTTPClient: NewMutatingRetryHTTPClient(),
	}

	resp.DataSourceData = client
	resp.ResourceData = client
	resp.ListResourceData = client
}

func (p *DevinProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewOrganizationResource,
		NewGitPermissionResource,
		NewPlaybookResource,
		NewKnowledgeNoteResource,
		NewSecretResource,
		NewScheduleResource,
		NewIPAccessListResource,
		NewOrgGroupLimitsResource,
		NewIdpGroupResource,
		NewEnterpriseIdpGroupRoleResource,
		NewOrgIdpGroupRoleResource,
		NewEnterpriseKnowledgeNoteResource,
		NewEnterprisePlaybookResource,
		NewOrgTagsResource,
		NewEnterpriseServiceUserRoleResource,
		NewOrgServiceUserRoleResource,
		NewOrgUserRoleResource,
	}
}

func (p *DevinProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewGitConnectionsDataSource,
		NewRolesDataSource,
		NewOrganizationsDataSource,
		NewUsersDataSource,
		NewServiceUsersDataSource,
		NewIdpGroupsDataSource,
	}
}

func (p *DevinProvider) ListResources(_ context.Context) []func() list.ListResource {
	return []func() list.ListResource{
		NewOrganizationListResource,
		NewPlaybookListResource,
		NewEnterprisePlaybookListResource,
		NewKnowledgeNoteListResource,
		NewEnterpriseKnowledgeNoteListResource,
		NewSecretListResource,
		NewScheduleListResource,
		NewIdpGroupListResource,
		NewGitPermissionListResource,
	}
}

func envOrDefault(attr types.String, envKey, fallback string) string {
	if !attr.IsNull() && !attr.IsUnknown() {
		return attr.ValueString()
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}
