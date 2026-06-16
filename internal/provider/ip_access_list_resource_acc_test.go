package provider

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func ipAccessListConfig(ranges string) string {
	return fmt.Sprintf(`%s
resource "devin_ip_access_list" "test" {
  ip_ranges = %s
}
`, providerConfig, ranges)
}

// checkIPAccessList asserts the API's IP access list contains exactly the
// given ranges (order-independent).
func checkIPAccessList(t *testing.T, want []string) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3/enterprise/ip-access-list", nil)
		if status != http.StatusOK {
			return fmt.Errorf("GET ip-access-list returned %d", status)
		}
		raw, _ := parsed["ip_ranges"].([]any)
		got := map[string]bool{}
		for _, r := range raw {
			if s, ok := r.(string); ok {
				got[s] = true
			}
		}
		if len(got) != len(want) {
			return fmt.Errorf("API has %d ranges, want %d", len(got), len(want))
		}
		for _, w := range want {
			if !got[w] {
				return fmt.Errorf("API missing expected range %q", w)
			}
		}
		return nil
	}
}

// loopbackCIDR is always included so the test runner (which calls the API from
// 127.0.0.1) does not lock itself out: the IP access list is enforced on the
// very next request, including the provider's own Read and the cross-checks.
const loopbackCIDR = "127.0.0.1/32"

// Create the list, update it, and confirm the API reflects each change. The
// list is cleared again on test teardown.
func TestAccIPAccessListResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             func(_ *terraform.State) error { return checkIPAccessList(t, nil)(nil) },
		Steps: []resource.TestStep{
			{
				Config: ipAccessListConfig(`["` + loopbackCIDR + `", "203.0.113.0/24"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_ip_access_list.test", "ip_ranges.#", "2"),
					checkIPAccessList(t, []string{loopbackCIDR, "203.0.113.0/24"}),
				),
			},
			{
				Config: ipAccessListConfig(`["` + loopbackCIDR + `", "198.51.100.0/24"]`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_ip_access_list.test", "ip_ranges.#", "2"),
					checkIPAccessList(t, []string{loopbackCIDR, "198.51.100.0/24"}),
				),
			},
		},
	})
}
