package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func orgConfig(name string, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
%s}
`, providerConfig, name, extra)
}

// Create with only the required field, verify computed/server-default fields,
// import, then rename in place.
func TestAccOrganizationResource_basic(t *testing.T) {
	name := randomName("tf-acc-org-basic")
	renamed := name + "-renamed"
	var orgID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: orgConfig(name, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_organization.test", "name", name),
					resource.TestCheckResourceAttrSet("devin_organization.test", "org_id"),
					// Server default applies when max_session_acu_limit is omitted.
					resource.TestCheckResourceAttrSet("devin_organization.test", "max_session_acu_limit"),
					resource.TestCheckNoResourceAttr("devin_organization.test", "max_cycle_acu_limit"),
					checkAPIFieldMatchesState("devin_organization.test", "/v3/enterprise/organizations/{org_id}", "name", "name"),
					checkAPIFieldMatchesState("devin_organization.test", "/v3/enterprise/organizations/{org_id}", "max_session_acu_limit", "max_session_acu_limit"),
					captureStateAttr("devin_organization.test", "org_id", &orgID),
				),
			},
			{
				ResourceName:                         "devin_organization.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "org_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_organization.test", "org_id"),
			},
			{
				Config: orgConfig(renamed, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_organization.test", "name", renamed),
					// Rename is an in-place update, not a replacement.
					resource.TestCheckResourceAttrPtr("devin_organization.test", "org_id", &orgID),
					checkAPIFieldMatchesState("devin_organization.test", "/v3/enterprise/organizations/{org_id}", "name", "name"),
				),
			},
		},
	})
}

// All optional fields set on create, then update one limit and clear the other.
func TestAccOrganizationResource_acuLimits(t *testing.T) {
	name := randomName("tf-acc-org-limits")
	var orgID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: orgConfig(name, "  max_session_acu_limit = 111\n  max_cycle_acu_limit   = 2222\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_organization.test", "max_session_acu_limit", "111"),
					resource.TestCheckResourceAttr("devin_organization.test", "max_cycle_acu_limit", "2222"),
					checkAPIFieldMatchesState("devin_organization.test", "/v3/enterprise/organizations/{org_id}", "max_session_acu_limit", "max_session_acu_limit"),
					checkAPIFieldMatchesState("devin_organization.test", "/v3/enterprise/organizations/{org_id}", "max_cycle_acu_limit", "max_cycle_acu_limit"),
					captureStateAttr("devin_organization.test", "org_id", &orgID),
				),
			},
			{
				// Update one limit in place and clear the other (merge-patch null).
				Config: orgConfig(name, "  max_session_acu_limit = 333\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_organization.test", "max_session_acu_limit", "333"),
					resource.TestCheckNoResourceAttr("devin_organization.test", "max_cycle_acu_limit"),
					resource.TestCheckResourceAttrPtr("devin_organization.test", "org_id", &orgID),
					checkAPIFieldMatchesState("devin_organization.test", "/v3/enterprise/organizations/{org_id}", "max_session_acu_limit", "max_session_acu_limit"),
				),
			},
			{
				ResourceName:                         "devin_organization.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "org_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_organization.test", "org_id"),
			},
		},
	})
}

// An org deleted directly via the API (out-of-band) must surface as a
// non-empty plan that recreates it, not as a refresh error.
func TestAccOrganizationResource_outOfBandDelete(t *testing.T) {
	name := randomName("tf-acc-org-oob")
	var orgID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: orgConfig(name, ""),
				Check:  captureStateAttr("devin_organization.test", "org_id", &orgID),
			},
			{
				PreConfig: func() {
					status, _ := testAccAPIRequest(t, http.MethodDelete, "/v3/enterprise/organizations/"+orgID, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band org delete returned %d", status)
					}
				},
				Config:             orgConfig(name, ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Importing an ID that does not exist must fail cleanly.
func TestAccOrganizationResource_importNonexistent(t *testing.T) {
	name := randomName("tf-acc-org-import-missing")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: orgConfig(name, ""),
			},
			{
				ResourceName:  "devin_organization.test",
				ImportState:   true,
				ImportStateId: "org-does-not-exist",
				ExpectError:   regexp.MustCompile(`Cannot import non-existent remote object`),
			},
		},
	})
}

// Enterprise-scoped operations must fail with an org-scoped token.
func TestAccOrganizationResource_orgScopedTokenForbidden(t *testing.T) {
	name := randomName("tf-acc-org-orgtoken")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}
`, orgTokenProviderConfig(), name),
				ExpectError: regexp.MustCompile(`(?i)403|forbidden|unauthorized|enterprise`),
			},
		},
	})
}

// Provider configuration errors: an empty token must fail before any API call,
// and a syntactically valid but unknown token must fail with an auth error.
func TestAccOrganizationResource_badProviderToken(t *testing.T) {
	name := randomName("tf-acc-org-badtoken")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
provider "devin" {
  token = ""
}

resource "devin_organization" "test" {
  name = %q
}
`, name),
				ExpectError: regexp.MustCompile(`Missing token`),
			},
			{
				// Correctly formatted token (base32 payload of 32 bytes) that
				// does not exist on the server.
				Config: fmt.Sprintf(`
provider "devin" {
  token = "cog_%s"
}

resource "devin_organization" "test" {
  name = %q
}
`, strings.Repeat("a", 52), name),
				ExpectError: regexp.MustCompile(`401|403|[Uu]nauthorized|[Ff]orbidden`),
			},
		},
	})
}
