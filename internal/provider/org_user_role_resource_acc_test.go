package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// mustFirstUserID returns a user_id from the enterprise membership.
func mustFirstUserID(t *testing.T) string {
	t.Helper()
	status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/members/users?first=1", nil)
	if status != http.StatusOK {
		t.Fatalf("GET users returned %d", status)
	}
	items, _ := parsed["items"].([]any)
	if len(items) == 0 {
		t.Fatal("enterprise has no users")
	}
	item, _ := items[0].(map[string]any)
	userID, _ := item["user_id"].(string)
	if userID == "" {
		t.Fatal("user listing returned no user_id")
	}
	return userID
}

// checkAPIUserOrgRole asserts the API reports the expected org role for the
// user ("" means no assignment in that org).
func checkAPIUserOrgRole(t *testing.T, userID string, orgID *string, wantRoleID string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/members/users/"+userID, nil)
		if status != http.StatusOK {
			return fmt.Errorf("GET user returned %d", status)
		}
		assignments, _ := parsed["role_assignments"].([]any)
		for _, raw := range assignments {
			assignment, _ := raw.(map[string]any)
			if assignment["org_id"] != *orgID {
				continue
			}
			role, _ := assignment["role"].(map[string]any)
			if wantRoleID == "" {
				return fmt.Errorf("user unexpectedly has role %v in org %s", role["role_id"], *orgID)
			}
			if role["role_id"] != wantRoleID {
				return fmt.Errorf("role_id in org %s = %v, want %s", *orgID, role["role_id"], wantRoleID)
			}
			return nil
		}
		if wantRoleID != "" {
			return fmt.Errorf("no role assignment found for org %s in %v", *orgID, assignments)
		}
		return nil
	}
}

// Add an enterprise user to a fresh org, import with the composite ID, switch
// the role, then verify destroy removes the user from the org.
func TestAccOrgUserRoleResource_basic(t *testing.T) {
	testAccSetupPreCheck(t)
	userID := mustFirstUserID(t)
	memberRoleID := mustRoleID(t, "org", "org_member")
	adminRoleID := mustRoleID(t, "org", "org_admin")
	orgName := randomName("tf-acc-userrole-org")
	var orgID string

	config := func(roleID string) string {
		return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_org_user_role" "test" {
  org_id  = devin_organization.test.org_id
  user_id = %q
  role_id = %q
}
`, providerConfig, orgName, userID, roleID)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(memberRoleID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_user_role.test", "user_id", userID),
					resource.TestCheckResourceAttr("devin_org_user_role.test", "role_id", memberRoleID),
					captureStateAttr("devin_org_user_role.test", "org_id", &orgID),
					checkAPIUserOrgRole(t, userID, &orgID, memberRoleID),
				),
			},
			{
				ResourceName:                         "devin_org_user_role.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "user_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_org_user_role.test", "org_id", "user_id"),
			},
			{
				Config: config(adminRoleID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_user_role.test", "role_id", adminRoleID),
					checkAPIUserOrgRole(t, userID, &orgID, adminRoleID),
				),
			},
		},
	})
}
