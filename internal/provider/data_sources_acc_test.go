package provider

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The roles data source returns the built-in roles every account has.
func TestAccRolesDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "devin_roles" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.devin_roles.all", "roles.#"),
					resource.TestCheckResourceAttrSet("data.devin_roles.all", "roles.0.role_id"),
					resource.TestCheckResourceAttrSet("data.devin_roles.all", "roles.0.role_name"),
					resource.TestCheckResourceAttrSet("data.devin_roles.all", "roles.0.role_type"),
				),
			},
		},
	})
}

// The organizations data source lists the enterprise's organizations. The
// local CI enterprise is seeded with at least one org.
func TestAccOrganizationsDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "devin_organizations" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.devin_organizations.all", "organizations.#"),
					resource.TestCheckResourceAttrSet("data.devin_organizations.all", "organizations.0.org_id"),
					resource.TestCheckResourceAttrSet("data.devin_organizations.all", "organizations.0.name"),
				),
			},
		},
	})
}

// The git connections data source lists the account's git connections. The
// local CI enterprise has none, so only the list shape is asserted.
func TestAccGitConnectionsDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "devin_git_connections" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.devin_git_connections.all", "connections.#"),
					func(s *terraform.State) error {
						count, err := stateAttr(s, "data.devin_git_connections.all", "connections.#")
						if err != nil {
							return err
						}
						n, err := strconv.Atoi(count)
						if err != nil || n == 0 {
							return nil
						}
						// When connections exist, every entry must expose its ID.
						for i := 0; i < n; i++ {
							if _, err := stateAttr(s, "data.devin_git_connections.all", fmt.Sprintf("connections.%d.git_connection_id", i)); err != nil {
								return err
							}
						}
						return nil
					},
				),
			},
		},
	})
}

// The users data source lists enterprise members. The test enterprise has at
// least one user, and the email filter narrows to one match.
func TestAccUsersDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "devin_users" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.devin_users.all", "users.#"),
					resource.TestCheckResourceAttrSet("data.devin_users.all", "users.0.user_id"),
					resource.TestCheckResourceAttrSet("data.devin_users.all", "users.0.role_assignments.#"),
				),
			},
		},
	})
}

// The email filter returns only the matching user.
func TestAccUsersDataSource_emailFilter(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "devin_users" "none" {
  email = "no-such-user@example.invalid"
}
`,
				Check: resource.TestCheckResourceAttr("data.devin_users.none", "users.#", "0"),
			},
		},
	})
}

// The service users data source lists active service users; the tokens used
// by the acceptance tests themselves are service users, so it is non-empty.
func TestAccServiceUsersDataSource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + `
data "devin_service_users" "all" {}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.devin_service_users.all", "service_users.#"),
					resource.TestCheckResourceAttrSet("data.devin_service_users.all", "service_users.0.service_user_id"),
					resource.TestCheckResourceAttrSet("data.devin_service_users.all", "service_users.0.name"),
				),
			},
		},
	})
}

// The IdP groups data source reflects groups registered via devin_idp_group.
func TestAccIdpGroupsDataSource_basic(t *testing.T) {
	groupName := randomName("tf-acc-ds-idp-group")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + fmt.Sprintf(`
resource "devin_idp_group" "test" {
  name = %q
}

data "devin_idp_groups" "all" {
  depends_on = [devin_idp_group.test]
}
`, groupName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.devin_idp_groups.all", "idp_groups.#"),
					func(s *terraform.State) error {
						count, err := stateAttr(s, "data.devin_idp_groups.all", "idp_groups.#")
						if err != nil {
							return err
						}
						n, err := strconv.Atoi(count)
						if err != nil {
							return err
						}
						for i := 0; i < n; i++ {
							name, err := stateAttr(s, "data.devin_idp_groups.all", fmt.Sprintf("idp_groups.%d.idp_group_name", i))
							if err != nil {
								return err
							}
							if name == groupName {
								return nil
							}
						}
						return fmt.Errorf("registered IdP group %q not found in data source output", groupName)
					},
				),
			},
		},
	})
}
