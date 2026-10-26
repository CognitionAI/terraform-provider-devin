package provider

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The Oncall API requires the Oncall flags and a completed Oncall setup for
// the target organization,
// so these tests run against DEVIN_ORG_TOKEN_ORG_ID with the org token.
// They use webhook:incoming triggers because they need no Slack, Teams or
// PagerDuty connection.

func oncallWebhookResponderConfig(name, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_oncall_responder" "test" {
  org_id   = %q
  name     = %q
  triggers = jsonencode([{ event_type = "webhook:incoming" }])
%s}
`, orgTokenProviderConfig(), os.Getenv("DEVIN_ORG_TOKEN_ORG_ID"), name, extra)
}

// The webhook secret is only returned when minted, so updates must carry it
// forward in state, and the JSON groups must round-trip through create,
// update (merge-patch with null-filled groups) and removal.
func TestAccOncallResponderResource_webhook(t *testing.T) {
	name := randomName("tf-acc-oncall-wh")
	var webhookSecret string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// The server trims the runbook, and session tags and the triage
				// issue type only reach it if Create sends them.
				Config: oncallWebhookResponderConfig(name, `
  triage_issue_type = "alerts"
  runbook = <<-EOT
    Check the dashboards.
  EOT
  session = jsonencode({ tags = ["tf-acc"] })
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkOncallResponderAPI(t, func(responder map[string]any) error {
						session, _ := responder["session"].(map[string]any)
						tags, _ := session["tags"].([]any)
						if responder["runbook"] != "Check the dashboards." || len(tags) != 1 || tags[0] != "tf-acc" ||
							responder["triage_issue_type"] != "alerts" {
							return fmt.Errorf("API responder does not match config: %v", responder)
						}
						return nil
					}),
					resource.TestCheckResourceAttrSet("devin_oncall_responder.test", "responder_id"),
					resource.TestCheckResourceAttr("devin_oncall_responder.test", "source", "webhook"),
					resource.TestCheckResourceAttr("devin_oncall_responder.test", "enabled", "true"),
					resource.TestCheckResourceAttrSet("devin_oncall_responder.test", "webhook_url"),
					resource.TestCheckResourceAttrSet("devin_oncall_responder.test", "webhook_secret"),
					captureStateAttr("devin_oncall_responder.test", "webhook_secret", &webhookSecret),
				),
			},
			{
				Config: oncallWebhookResponderConfig(name+"-v2", `
  triage_issue_type = "alerts"
  enabled          = false
  runbook          = "Check the dashboards, then summarize."
  metadata         = { team = "platform" }
  limits           = jsonencode({ max_acu_limit = 10, invocations = { max_per_window = 5, window_seconds = 3600 } })
  session_settings = jsonencode({ devin_mode = "fast" })
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_oncall_responder.test", "name", name+"-v2"),
					resource.TestCheckResourceAttrPtr("devin_oncall_responder.test", "webhook_secret", &webhookSecret),
					checkOncallResponderAPI(t, func(responder map[string]any) error {
						limits, _ := responder["limits"].(map[string]any)
						if responder["enabled"] != false || responder["runbook"] != "Check the dashboards, then summarize." ||
							limits == nil || limits["max_acu_limit"] != float64(10) {
							return fmt.Errorf("API responder does not match config: %v", responder)
						}
						return nil
					}),
				),
			},
			{
				// Removing a group sends an explicit null so the server clears it.
				Config: oncallWebhookResponderConfig(name+"-v2", `
  triage_issue_type = "alerts"
  enabled          = false
  runbook          = "Check the dashboards, then summarize."
  session_settings = jsonencode({ devin_mode = "fast" })
`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_oncall_responder.test", "limits"),
					resource.TestCheckNoResourceAttr("devin_oncall_responder.test", "metadata"),
					checkOncallResponderAPI(t, func(responder map[string]any) error {
						if responder["limits"] != nil {
							return fmt.Errorf("expected limits to be cleared, got %v", responder["limits"])
						}
						return nil
					}),
				),
			},
			{
				ResourceName:                         "devin_oncall_responder.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_oncall_responder.test", "org_id", "responder_id"),
				ImportStateVerifyIdentifierAttribute: "responder_id",
				ImportStateVerifyIgnore:              []string{"triggers", "session_settings", "webhook_secret"},
			},
		},
	})
}

// checkOncallResponderAPI fetches the responder with the org token (the
// enterprise token has no Oncall permissions) and passes it to verify.
func checkOncallResponderAPI(t *testing.T, verify func(responder map[string]any) error) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		orgID, err := stateAttr(s, "devin_oncall_responder.test", "org_id")
		if err != nil {
			return err
		}
		responderID, err := stateAttr(s, "devin_oncall_responder.test", "responder_id")
		if err != nil {
			return err
		}
		status, responder := testAccAPIRequestWithToken(t, http.MethodGet,
			"/v3/organizations/"+orgID+"/oncall/responders/"+responderID, nil, os.Getenv("DEVIN_ORG_TOKEN"))
		if status != http.StatusOK {
			return fmt.Errorf("GET responder returned %d: %v", status, responder)
		}
		return verify(responder)
	}
}
