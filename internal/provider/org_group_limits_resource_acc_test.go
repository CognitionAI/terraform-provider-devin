package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func orgGroupLimitsConfig(orgName, groupName string, maxCycleACUs string) string {
	extra := ""
	if maxCycleACUs != "" {
		extra = "\n    max_cycle_acus = " + maxCycleACUs
	}
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_org_group_limits" "test" {
  groups = [
    {
      name    = %q
      org_ids = [devin_organization.test.org_id]%s
    },
  ]
}
`, providerConfig, orgName, groupName, extra)
}

// checkOrgGroupLimits asserts the API reports the expected group with the
// given max_cycle_acus (-1 means "unset").
func checkOrgGroupLimits(t *testing.T, groupName string, wantMaxCycleACUs int) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/org-group-limits", nil)
		if status != http.StatusOK {
			return fmt.Errorf("GET org-group-limits returned %d", status)
		}
		groups, _ := parsed["groups"].(map[string]any)
		group, ok := groups[groupName].(map[string]any)
		if !ok {
			return fmt.Errorf("group %q not present in API config", groupName)
		}
		if wantMaxCycleACUs < 0 {
			if v, present := group["max_cycle_acus"]; present && v != nil {
				return fmt.Errorf("group %q expected no max_cycle_acus, got %v", groupName, v)
			}
			return nil
		}
		v, ok := group["max_cycle_acus"].(float64)
		if !ok || int(v) != wantMaxCycleACUs {
			return fmt.Errorf("group %q max_cycle_acus = %v, want %d", groupName, group["max_cycle_acus"], wantMaxCycleACUs)
		}
		return nil
	}
}

// Create a group with a cycle limit, then update the limit, confirming the API
// reflects each change. The config is cleared on teardown.
func TestAccOrgGroupLimitsResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-oggroup-org")
	groupName := randomName("tf-acc-group")

	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			// org-group-limits is gated by the enterprise-org-group-limits
			// feature flag; skip when the API returns 404 (flag off).
			if status, _ := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/org-group-limits", nil); status == http.StatusNotFound {
				t.Skip("enterprise-org-group-limits feature flag is not enabled on the target API")
			}
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(_ *terraform.State) error {
			status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/org-group-limits", nil)
			if status != http.StatusOK {
				return nil
			}
			groups, _ := parsed["groups"].(map[string]any)
			if _, ok := groups[groupName]; ok {
				return fmt.Errorf("group %q still present after destroy", groupName)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: orgGroupLimitsConfig(orgName, groupName, "5000"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_org_group_limits.test", "groups.#", "1"),
					checkOrgGroupLimits(t, groupName, 5000),
				),
			},
			{
				Config: orgGroupLimitsConfig(orgName, groupName, "8000"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkOrgGroupLimits(t, groupName, 8000),
				),
			},
		},
	})
}
