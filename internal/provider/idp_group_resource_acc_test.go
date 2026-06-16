package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func idpGroupConfig(name string) string {
	return fmt.Sprintf(`%s
resource "devin_idp_group" "test" {
  name = %q
}
`, providerConfig, name)
}

func checkIdpGroupInAPI(t *testing.T, name string, expectExists bool) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/idp-groups?first=100", nil)
		if status != http.StatusOK {
			return fmt.Errorf("listing idp groups returned %d", status)
		}
		items, _ := parsed["items"].([]any)
		found := false
		for _, raw := range items {
			if item, ok := raw.(map[string]any); ok && item["idp_group_name"] == name {
				found = true
				break
			}
		}
		if found != expectExists {
			return fmt.Errorf("idp group %q exists = %t, expected %t", name, found, expectExists)
		}
		return nil
	}
}

func TestAccIdpGroupResource_basic(t *testing.T) {
	name := randomName("tf-acc-idp-group")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             checkIdpGroupInAPI(t, name, false),
		Steps: []resource.TestStep{
			{
				Config: idpGroupConfig(name),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_idp_group.test", "name", name),
					checkIdpGroupInAPI(t, name, true),
				),
			},
			{
				ResourceName:                         "devin_idp_group.test",
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "name",
			},
		},
	})
}

// Register an enterprise IdP group and map it to an account-level role. The
// account role ID is resolved from the roles data source so the test does not
// depend on a hardcoded ID.
func TestAccEnterpriseIdpGroupRoleResource_basic(t *testing.T) {
	name := randomName("tf-acc-idp-role")

	config := fmt.Sprintf(`%s
data "devin_roles" "all" {}

locals {
  account_role_id = [for r in data.devin_roles.all.roles : r.role_id if r.role_type == "enterprise"][0]
}

resource "devin_idp_group" "test" {
  name = %q
}

resource "devin_enterprise_idp_group_role" "test" {
  idp_group_name = devin_idp_group.test.name
  role_id        = local.account_role_id
}
`, providerConfig, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_enterprise_idp_group_role.test", "role_id"),
					checkIdpGroupHasAccountRole(t, name),
				),
			},
			{
				ResourceName:                         "devin_enterprise_idp_group_role.test",
				ImportState:                          true,
				ImportStateId:                        name,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "idp_group_name",
			},
		},
	})
}

// Register an IdP group and map it to an org-level role scoped to a freshly
// created organization.
func TestAccOrgIdpGroupRoleResource_basic(t *testing.T) {
	name := randomName("tf-acc-idp-org-role")

	// The API rejects an org-scoped assignment for a group with no prior role
	// assignments, so an enterprise role is assigned first.
	config := fmt.Sprintf(`%s
data "devin_roles" "all" {}

locals {
  account_role_id = [for r in data.devin_roles.all.roles : r.role_id if r.role_type == "enterprise"][0]
  org_role_id     = [for r in data.devin_roles.all.roles : r.role_id if r.role_type == "org"][0]
}

resource "devin_organization" "test" {
  name = %q
}

resource "devin_idp_group" "test" {
  name = %q
}

resource "devin_enterprise_idp_group_role" "test" {
  idp_group_name = devin_idp_group.test.name
  role_id        = local.account_role_id
}

resource "devin_org_idp_group_role" "test" {
  org_id         = devin_organization.test.org_id
  idp_group_name = devin_idp_group.test.name
  role_id        = local.org_role_id

  depends_on = [devin_enterprise_idp_group_role.test]
}
`, providerConfig, name, name)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_org_idp_group_role.test", "role_id"),
					resource.TestCheckResourceAttrSet("devin_org_idp_group_role.test", "org_id"),
				),
			},
			{
				ResourceName:                         "devin_org_idp_group_role.test",
				ImportState:                          true,
				ImportStateIdFunc:                    orgIdpGroupRoleImportID(name),
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "idp_group_name",
			},
		},
	})
}

// orgIdpGroupRoleImportID builds the "org_id/idp_group_name" import ID from
// the org created during the test.
func orgIdpGroupRoleImportID(groupName string) resource.ImportStateIdFunc {
	return func(state *terraform.State) (string, error) {
		rs, ok := state.RootModule().Resources["devin_organization.test"]
		if !ok {
			return "", fmt.Errorf("devin_organization.test not found in state")
		}
		return rs.Primary.Attributes["org_id"] + "/" + groupName, nil
	}
}

// checkIdpGroupHasAccountRole asserts the group has an enterprise (org_id
// null) role assignment in the API.
func checkIdpGroupHasAccountRole(t *testing.T, name string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/members/idp-groups/"+name, nil)
		if status != http.StatusOK {
			return fmt.Errorf("GET idp-group %q returned %d", name, status)
		}
		assignments, _ := parsed["role_assignments"].([]any)
		for _, raw := range assignments {
			if a, ok := raw.(map[string]any); ok && a["org_id"] == nil {
				return nil
			}
		}
		return fmt.Errorf("idp group %q has no enterprise role assignment", name)
	}
}
