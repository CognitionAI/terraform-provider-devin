package provider

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// mustRoleID returns the role_id of an enterprise role with the requested
// type, preferring the given identifiers in order. Each preferred string is
// matched against both role_id and role_name so callers can find per-account
// default roles (which have generated IDs but a well-known name like "Member").
func mustRoleID(t *testing.T, roleType string, preferred ...string) string {
	t.Helper()
	status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/roles?first=100", nil)
	if status != http.StatusOK {
		t.Fatalf("GET roles returned %d", status)
	}
	items, _ := parsed["items"].([]any)
	byID := map[string]bool{}
	byName := map[string]string{}
	var fallback string
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["role_type"] != roleType {
			continue
		}
		roleID, _ := item["role_id"].(string)
		roleName, _ := item["role_name"].(string)
		byID[roleID] = true
		if roleName != "" {
			byName[roleName] = roleID
		}
		if fallback == "" {
			fallback = roleID
		}
	}
	for _, want := range preferred {
		if byID[want] {
			return want
		}
		if id, ok := byName[want]; ok {
			return id
		}
	}
	if fallback == "" {
		t.Fatalf("enterprise has no %s roles", roleType)
	}
	return fallback
}

// provisionEnterpriseServiceUser creates an enterprise-scoped service user
// (it has an account-level role but no org roles) via the beta provisioning
// endpoint.
func provisionEnterpriseServiceUser(t *testing.T) string {
	t.Helper()
	body := map[string]any{
		"name":        randomName("tf-acc-su-ent"),
		"role_id":     mustRoleID(t, "enterprise", "account_member", "Member"),
		"ttl_seconds": 3600,
	}
	status, parsed := testAccAPIRequest(t, http.MethodPost, "/v3beta1/enterprise/service-users", body)
	if status != http.StatusOK {
		t.Fatalf("provisioning enterprise service user returned %d: %v", status, parsed)
	}
	su, _ := parsed["service_user"].(map[string]any)
	suID, _ := su["service_user_id"].(string)
	if suID == "" {
		t.Fatalf("provisioning enterprise service user returned no service_user_id: %v", parsed)
	}
	return suID
}

// checkAPIServiceUserRole asserts the API reports the expected role for the
// service user at the given scope ("" means the account scope).
func checkAPIServiceUserRole(t *testing.T, serviceUserID *string, orgID *string, wantRoleID string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/members/service-users/"+*serviceUserID, nil)
		if status != http.StatusOK {
			return fmt.Errorf("GET service user returned %d", status)
		}
		assignments, _ := parsed["role_assignments"].([]any)
		for _, raw := range assignments {
			assignment, _ := raw.(map[string]any)
			scopeOrg, _ := assignment["org_id"].(string)
			wantOrg := ""
			if orgID != nil {
				wantOrg = *orgID
			}
			if scopeOrg != wantOrg {
				continue
			}
			role, _ := assignment["role"].(map[string]any)
			if role["role_id"] != wantRoleID {
				return fmt.Errorf("role_id at scope %q = %v, want %s", wantOrg, role["role_id"], wantRoleID)
			}
			return nil
		}
		return fmt.Errorf("no role assignment found at the expected scope in %v", assignments)
	}
}

// Assign an enterprise role to an org-scoped service user, import, then
// switch the role. The target service user is seeded by the local API
// harness (DEVIN_ROLE_SU_ID): it has an org role but no account-level role,
// which cannot be provisioned through the API by the test token. Destroy is
// a state-only removal (the API cannot drop an account role without
// deleting the service user), so the assignment is expected to survive the
// destroy.
func TestAccEnterpriseServiceUserRoleResource_basic(t *testing.T) {
	testAccSetupPreCheck(t)
	suID := os.Getenv("DEVIN_ROLE_SU_ID")
	if suID == "" {
		t.Skip("DEVIN_ROLE_SU_ID must be set to a service user without an account-level role")
	}
	memberRoleID := mustRoleID(t, "enterprise", "account_member", "Member")
	adminRoleID := mustRoleID(t, "enterprise", "account_admin", "Admin")

	config := func(roleID string) string {
		return fmt.Sprintf(`%s
resource "devin_enterprise_service_user_role" "test" {
  service_user_id = %q
  role_id         = %q
}
`, providerConfig, suID, roleID)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(memberRoleID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_enterprise_service_user_role.test", "service_user_id", suID),
					resource.TestCheckResourceAttr("devin_enterprise_service_user_role.test", "role_id", memberRoleID),
					checkAPIServiceUserRole(t, &suID, nil, memberRoleID),
				),
			},
			{
				ResourceName:                         "devin_enterprise_service_user_role.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "service_user_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_enterprise_service_user_role.test", "service_user_id"),
			},
			{
				Config: config(adminRoleID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_enterprise_service_user_role.test", "role_id", adminRoleID),
					checkAPIServiceUserRole(t, &suID, nil, adminRoleID),
				),
			},
		},
		// Destroy leaves the assignment in place by design.
		CheckDestroy: func(_ *terraform.State) error {
			return checkAPIServiceUserRole(t, &suID, nil, adminRoleID)(nil)
		},
	})
}

// Assign an org role to an enterprise-scoped service user in a fresh org,
// import with the composite ID, switch the role, then verify destroy removes
// the assignment.
func TestAccOrgServiceUserRoleResource_basic(t *testing.T) {
	testAccSetupPreCheck(t)
	suID := provisionEnterpriseServiceUser(t)
	memberRoleID := mustRoleID(t, "org", "org_member", "Member")
	adminRoleID := mustRoleID(t, "org", "org_admin", "Admin")
	orgName := randomName("tf-acc-surole-org")
	var orgID string

	config := func(roleID string) string {
		return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_org_service_user_role" "test" {
  org_id          = devin_organization.test.org_id
  service_user_id = %q
  role_id         = %q
}
`, providerConfig, orgName, suID, roleID)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(memberRoleID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_service_user_role.test", "service_user_id", suID),
					resource.TestCheckResourceAttr("devin_org_service_user_role.test", "role_id", memberRoleID),
					captureStateAttr("devin_org_service_user_role.test", "org_id", &orgID),
					checkAPIServiceUserRole(t, &suID, &orgID, memberRoleID),
				),
			},
			{
				ResourceName:                         "devin_org_service_user_role.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "service_user_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_org_service_user_role.test", "org_id", "service_user_id"),
			},
			{
				Config: config(adminRoleID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_service_user_role.test", "role_id", adminRoleID),
					checkAPIServiceUserRole(t, &suID, &orgID, adminRoleID),
				),
			},
		},
	})
}
