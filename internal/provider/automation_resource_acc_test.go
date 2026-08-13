package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// The automations API requires the automations feature to be enabled for the
// target organization.
//
// The tests use schedule:recurring triggers and start_session actions because
// they need no git connection, Slack workspace, or other external setup.
// run_as = "creator" and email notifications are rejected for
// service-user-created automations (the acceptance tests authenticate as a
// service user), so they are not exercised here.

func automationConfig(orgName, name, extra string) string {
	return automationConfigWithPrompt(orgName, name, "Run the nightly check", extra)
}

func automationConfigWithPrompt(orgName, name, prompt, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_automation" "test" {
  org_id = devin_organization.test.org_id
  name   = %q
  triggers = jsonencode([{
    event_type = "schedule:recurring"
    conditions = {
      any = [{ all = [{ field = "rrule", operator = "recurrence", value = "FREQ=DAILY;BYHOUR=9;BYMINUTE=0" }] }]
    }
  }])
  actions = jsonencode([{ type = "start_session", prompt = %q }])
%s}
`, providerConfig, orgName, name, prompt, extra)
}

func automationConfigWithActions(orgName, name, actions string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_automation" "test" {
  org_id = devin_organization.test.org_id
  name   = %q
  triggers = jsonencode([{
    event_type = "schedule:recurring"
    conditions = {
      any = [{ all = [{ field = "rrule", operator = "recurrence", value = "FREQ=DAILY;BYHOUR=9;BYMINUTE=0" }] }]
    }
  }])
  actions = jsonencode(%s)
}
`, providerConfig, orgName, name, actions)
}

func automationConfigWithTriggers(orgName, name, triggers, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_automation" "test" {
  org_id = devin_organization.test.org_id
  name   = %q
  triggers = jsonencode(%s)
  actions = jsonencode([{ type = "start_session", prompt = "Run the nightly check" }])
%s}
`, providerConfig, orgName, name, triggers, extra)
}

// automationImportVerifyIgnore lists the attributes Read does not refresh
// (config-authoritative JSON groups) plus the write-only webhook secret; a
// fresh import cannot recover them.
var automationImportVerifyIgnore = []string{
	"triggers", "actions", "notifications", "limits", "concurrency", "tools",
	"session_settings", "run_as", "webhook_secret",
}

// Create with a schedule trigger, validate against the API, import with the
// composite ID, update scalars and groups in place, then clear the optional
// groups and verify the API side is cleared too.
func TestAccAutomationResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-auto-org")
	var automationID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "Nightly check", "  metadata    = { env = \"dev\" }\n  limits      = jsonencode({ max_acu_limit = 25, invocations = { max_per_window = 5, window_seconds = 3600 } })\n  concurrency = jsonencode({ max_concurrent_runs = 2 })\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_automation.test", "automation_id"),
					resource.TestCheckResourceAttr("devin_automation.test", "name", "Nightly check"),
					resource.TestCheckResourceAttr("devin_automation.test", "enabled", "true"),
					resource.TestCheckResourceAttr("devin_automation.test", "metadata.env", "dev"),
					resource.TestCheckNoResourceAttr("devin_automation.test", "webhook_url"),
					resource.TestCheckResourceAttrPair("devin_automation.test", "org_id", "devin_organization.test", "org_id"),
					checkAPIFieldMatchesState("devin_automation.test", "/v3/organizations/{org_id}/automations/{automation_id}", "name", "name"),
					checkAPIFieldMatchesState("devin_automation.test", "/v3/organizations/{org_id}/automations/{automation_id}", "enabled", "enabled"),
					captureStateAttr("devin_automation.test", "automation_id", &automationID),
				),
			},
			{
				ResourceName:                         "devin_automation.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "automation_id",
				ImportStateVerifyIgnore:              automationImportVerifyIgnore,
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_automation.test", "org_id", "automation_id"),
			},
			{
				// In-place update: rename, disable, replace the metadata key.
				Config: automationConfig(orgName, "Nightly check v2", "  enabled  = false\n  metadata = { tier = \"gold\" }\n  limits   = jsonencode({ max_acu_limit = 50 })\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_automation.test", "name", "Nightly check v2"),
					resource.TestCheckResourceAttr("devin_automation.test", "enabled", "false"),
					resource.TestCheckResourceAttr("devin_automation.test", "metadata.tier", "gold"),
					resource.TestCheckNoResourceAttr("devin_automation.test", "metadata.env"),
					resource.TestCheckResourceAttrPtr("devin_automation.test", "automation_id", &automationID),
					checkAPIFieldMatchesState("devin_automation.test", "/v3/organizations/{org_id}/automations/{automation_id}", "name", "name"),
					checkAPIFieldMatchesState("devin_automation.test", "/v3/organizations/{org_id}/automations/{automation_id}", "enabled", "enabled"),
					checkAutomationNestedAPIFieldNull("devin_automation.test", "limits", "invocations"),
				),
			},
			{
				// Removing the optional groups must clear them on the API side
				// (metadata deletions are sent as explicit per-key nulls).
				Config: automationConfig(orgName, "Nightly check v2", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_automation.test", "metadata"),
					resource.TestCheckResourceAttrPtr("devin_automation.test", "automation_id", &automationID),
					checkAutomationAPIFieldNull("devin_automation.test", "limits"),
					checkAutomationAPIFieldNull("devin_automation.test", "concurrency"),
					checkAutomationAPIFieldEmptyObject("devin_automation.test", "metadata"),
				),
			},
		},
	})
}

func TestAccAutomationResource_nameOnlyPreservesTriggerID(t *testing.T) {
	orgName := randomName("tf-acc-auto-trigger-id")
	var orgID, automationID, triggerID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "Trigger ID check", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_automation.test", "org_id", &orgID),
					captureStateAttr("devin_automation.test", "automation_id", &automationID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/automations/%s", orgID, automationID)
					status, response := testAccAPIRequest(t, http.MethodGet, path, nil)
					if status != http.StatusOK {
						t.Fatalf("pre-update automation GET returned %d", status)
					}
					triggers, ok := response["triggers"].([]any)
					if !ok || len(triggers) == 0 {
						t.Fatalf("expected pre-update API triggers, got %v", response["triggers"])
					}
					trigger, ok := triggers[0].(map[string]any)
					if !ok {
						t.Fatalf("expected pre-update trigger object, got %T", triggers[0])
					}
					triggerID, ok = trigger["trigger_id"].(string)
					if !ok || triggerID == "" {
						t.Fatalf("expected pre-update trigger_id, got %v", trigger["trigger_id"])
					}
				},
				Config: automationConfig(orgName, "Trigger ID check renamed", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAutomationTriggerID("devin_automation.test", &triggerID),
				),
			},
		},
	})
}

func TestAccAutomationResource_emptyMetadataStable(t *testing.T) {
	orgName := randomName("tf-acc-auto-empty-metadata")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "Empty metadata", "  metadata = {}\n"),
			},
			{
				Config:             automationConfig(orgName, "Empty metadata", "  metadata = {}\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: false,
			},
		},
	})
}

func TestAccAutomationResource_sessionSettingsNetPolicyUpdate(t *testing.T) {
	orgName := randomName("tf-acc-auto-net-policy")
	netPolicy := "  session_settings = jsonencode({ net_policy = { allow = [{ hostname = \"example.com\" }] } })\n"
	updatedNetPolicy := "  session_settings = jsonencode({ devin_mode = \"normal\", net_policy = { allow = [{ hostname = \"example.com\" }] } })\n"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "Net policy check", netPolicy),
			},
			{
				Config: automationConfig(orgName, "Net policy check updated", updatedNetPolicy),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAutomationNestedAPIFieldPresent("devin_automation.test", "session_settings", "net_policy"),
				),
			},
		},
	})
}

func TestAccAutomationResource_actionPromptUpdate(t *testing.T) {
	orgName := randomName("tf-acc-auto-action-prompt")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfigWithPrompt(orgName, "Action prompt check", "Initial prompt", ""),
			},
			{
				Config: automationConfigWithPrompt(orgName, "Action prompt check", "Updated prompt", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAutomationNestedAPIFieldValue("devin_automation.test", "actions", "prompt", "Updated prompt"),
				),
			},
		},
	})
}

func TestAccAutomationResource_actionSessionRemoval(t *testing.T) {
	orgName := randomName("tf-acc-auto-action-session")
	withSession := `[{ type = "start_session", prompt = "Run the nightly check", session = { tags = ["probe"] } }]`
	withoutSession := `[{ type = "start_session", prompt = "Run the nightly check" }]`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfigWithActions(orgName, "Session removal check", withSession),
			},
			{
				Config: automationConfigWithActions(orgName, "Session removal check", withoutSession),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAutomationActionSessionTagsEmpty("devin_automation.test"),
				),
			},
		},
	})
}

func TestAccAutomationResource_toolsMCPRemoval(t *testing.T) {
	orgName := randomName("tf-acc-auto-tools")
	initialTools := "  tools = jsonencode({ mcp_servers = [\"github\"] })\n"
	removedTools := "  tools = jsonencode({})\n"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "Tools removal check", initialTools),
			},
			{
				Config: automationConfig(orgName, "Tools removal check", removedTools),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAutomationNestedAPIFieldEmptyArray("devin_automation.test", "tools", "mcp_servers"),
				),
			},
		},
	})
}

func TestAccAutomationResource_triggerConditionsRemoval(t *testing.T) {
	orgName := randomName("tf-acc-auto-trigger-conditions")
	initialTriggers := `[{ event_type = "incident_io:incident_created", conditions = {
  any = [{ all = [{ field = "incident.name", operator = "contains", value = "probe" }] }]
} }]`
	removedConditions := `[{ event_type = "incident_io:incident_created" }]`

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfigWithTriggers(orgName, "Trigger conditions check", initialTriggers, ""),
			},
			{
				Config: automationConfigWithTriggers(orgName, "Trigger conditions check", removedConditions, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkAutomationTriggerConditionsNull("devin_automation.test"),
				),
			},
		},
	})
}

// A webhook:incoming trigger mints an inbox URL and secret on create; the
// secret is never returned again, so updates must preserve it in state.
func TestAccAutomationResource_webhook(t *testing.T) {
	orgName := randomName("tf-acc-auto-wh-org")
	var webhookSecret string

	webhookConfig := func(name string) string {
		return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_automation" "test" {
  org_id   = devin_organization.test.org_id
  name     = %q
  triggers = jsonencode([{ event_type = "webhook:incoming" }])
  actions  = jsonencode([{ type = "start_session", prompt = "Handle the webhook" }])
}
`, providerConfig, orgName, name)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: webhookConfig("Webhook automation"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_automation.test", "webhook_url"),
					resource.TestCheckResourceAttrSet("devin_automation.test", "webhook_secret"),
					captureStateAttr("devin_automation.test", "webhook_secret", &webhookSecret),
				),
			},
			{
				// A scalar-only update re-sends the triggers; the API keeps the
				// existing secret and the provider must carry it forward.
				Config: webhookConfig("Webhook automation v2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_automation.test", "name", "Webhook automation v2"),
					resource.TestCheckResourceAttrSet("devin_automation.test", "webhook_url"),
					resource.TestCheckResourceAttrPtr("devin_automation.test", "webhook_secret", &webhookSecret),
				),
			},
			{
				// Removing the webhook trigger clears the URL and secret; the
				// plan must mark them unknown rather than carry the prior
				// values forward, or the apply fails as inconsistent.
				Config: automationConfigWithTriggers(orgName, "Webhook automation v3",
					`[{ event_type = "schedule:recurring", conditions = { any = [{ all = [{ field = "rrule", operator = "recurrence", value = "FREQ=DAILY;BYHOUR=9;BYMINUTE=0" }] }] } }]`, ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_automation.test", "webhook_url"),
					resource.TestCheckNoResourceAttr("devin_automation.test", "webhook_secret"),
				),
			},
		},
	})
}

// Config typos in the JSON groups must fail locally at plan/apply time rather
// than being silently dropped from the request.
func TestAccAutomationResource_unknownTriggerKeyRejected(t *testing.T) {
	orgName := randomName("tf-acc-auto-typo")

	config := fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_automation" "test" {
  org_id   = devin_organization.test.org_id
  name     = "Typo automation"
  triggers = jsonencode([{ event_type = "schedule:recurring", replies_typo = [] }])
  actions  = jsonencode([{ type = "start_session", prompt = "x" }])
}
`, providerConfig, orgName)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`(?i)unknown field`),
			},
		},
	})
}

// Malformed composite import IDs must be rejected.
func TestAccAutomationResource_importInvalidID(t *testing.T) {
	orgName := randomName("tf-acc-auto-import")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "Import test", ""),
			},
			{
				ResourceName:  "devin_automation.test",
				ImportState:   true,
				ImportStateId: "missing-a-slash",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
	})
}

// An automation deleted out-of-band must plan a recreate, not error.
func TestAccAutomationResource_outOfBandDelete(t *testing.T) {
	orgName := randomName("tf-acc-auto-oob")
	var orgID, automationID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: automationConfig(orgName, "OOB delete", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_automation.test", "org_id", &orgID),
					captureStateAttr("devin_automation.test", "automation_id", &automationID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/automations/%s", orgID, automationID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band automation delete returned %d", status)
					}
				},
				Config:             automationConfig(orgName, "OOB delete", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// checkAutomationAPIFieldNull asserts that fetching the automation returns a
// null (or absent) value for the given field.
func checkAutomationAPIFieldNull(resourceName, apiField string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, apiField, func(value any) error {
		if value != nil {
			return fmt.Errorf("expected API field %s to be null, got %v", apiField, value)
		}
		return nil
	})
}

// checkAutomationAPIFieldEmptyObject asserts that fetching the automation
// returns an absent or empty object for the given field.
func checkAutomationAPIFieldEmptyObject(resourceName, apiField string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, apiField, func(value any) error {
		if value == nil {
			return nil
		}
		obj, ok := value.(map[string]any)
		if !ok || len(obj) != 0 {
			return fmt.Errorf("expected API field %s to be empty, got %v", apiField, value)
		}
		return nil
	})
}

func checkAutomationNestedAPIFieldNull(resourceName, apiField, nestedField string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, apiField, func(value any) error {
		if value == nil {
			return nil
		}
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected API field %s to be an object, got %T", apiField, value)
		}
		if nested, exists := object[nestedField]; exists && nested != nil {
			return fmt.Errorf("expected API field %s.%s to be null or absent, got %v", apiField, nestedField, nested)
		}
		return nil
	})
}

func checkAutomationNestedAPIFieldPresent(resourceName, apiField, nestedField string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, apiField, func(value any) error {
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected API field %s to be an object, got %T", apiField, value)
		}
		if nested, exists := object[nestedField]; !exists || nested == nil {
			return fmt.Errorf("expected API field %s.%s to be present, got %v", apiField, nestedField, nested)
		}
		return nil
	})
}

func checkAutomationNestedAPIFieldValue(resourceName, apiField, nestedField string, expected string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, apiField, func(value any) error {
		objects, ok := value.([]any)
		if !ok || len(objects) == 0 {
			return fmt.Errorf("expected API field %s to contain an object, got %v", apiField, value)
		}
		object, ok := objects[0].(map[string]any)
		if !ok || object[nestedField] != expected {
			return fmt.Errorf("expected API field %s[0].%s to be %q, got %v", apiField, nestedField, expected, object[nestedField])
		}
		return nil
	})
}

func checkAutomationNestedAPIFieldEmptyArray(resourceName, apiField, nestedField string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, apiField, func(value any) error {
		if value == nil {
			return nil
		}
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected API field %s to be an object, got %T", apiField, value)
		}
		nested, ok := object[nestedField].([]any)
		if !ok || len(nested) != 0 {
			return fmt.Errorf("expected API field %s.%s to be empty, got %v", apiField, nestedField, object[nestedField])
		}
		return nil
	})
}

func checkAutomationActionSessionTagsEmpty(resourceName string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, "actions", func(value any) error {
		actions, ok := value.([]any)
		if !ok || len(actions) == 0 {
			return fmt.Errorf("expected API actions to contain at least one action, got %v", value)
		}
		action, ok := actions[0].(map[string]any)
		if !ok {
			return fmt.Errorf("expected API action object, got %T", actions[0])
		}
		session, ok := action["session"].(map[string]any)
		if !ok {
			return fmt.Errorf("expected API action session object, got %v", action["session"])
		}
		if tags, ok := session["tags"].([]any); !ok || len(tags) != 0 {
			return fmt.Errorf("expected action session tags to be empty, got %v", session["tags"])
		}
		return nil
	})
}

func checkAutomationTriggerConditionsNull(resourceName string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, "triggers", func(value any) error {
		triggers, ok := value.([]any)
		if !ok || len(triggers) == 0 {
			return fmt.Errorf("expected API triggers to contain at least one trigger, got %v", value)
		}
		trigger, ok := triggers[0].(map[string]any)
		if !ok {
			return fmt.Errorf("expected API trigger object, got %T", triggers[0])
		}
		if conditions, exists := trigger["conditions"]; exists && conditions != nil {
			return fmt.Errorf("expected trigger conditions to be null or absent, got %v", conditions)
		}
		return nil
	})
}

func checkAutomationTriggerID(resourceName string, expected *string) resource.TestCheckFunc {
	return checkAutomationAPIField(resourceName, "triggers", func(value any) error {
		triggers, ok := value.([]any)
		if !ok || len(triggers) == 0 {
			return fmt.Errorf("expected API triggers to contain at least one trigger, got %v", value)
		}
		trigger, ok := triggers[0].(map[string]any)
		if !ok {
			return fmt.Errorf("expected API trigger object, got %T", triggers[0])
		}
		actual, ok := trigger["trigger_id"].(string)
		if !ok || actual != *expected {
			return fmt.Errorf("expected API trigger_id %q, got %v", *expected, trigger["trigger_id"])
		}
		return nil
	})
}

func checkAutomationAPIField(resourceName, apiField string, verify func(value any) error) resource.TestCheckFunc {
	return func(s *terraform.State) (retErr error) {
		orgID, err := stateAttr(s, resourceName, "org_id")
		if err != nil {
			return err
		}
		automationID, err := stateAttr(s, resourceName, "automation_id")
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/v3/organizations/%s/automations/%s", orgID, automationID)

		url := strings.TrimRight(os.Getenv("DEVIN_API_URL"), "/") + path
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+os.Getenv("DEVIN_TOKEN"))
		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil && retErr == nil {
				retErr = closeErr
			}
		}()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GET %s returned %d", path, resp.StatusCode)
		}
		var parsed map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			return err
		}
		return verify(parsed[apiField])
	}
}
