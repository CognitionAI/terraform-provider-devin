package provider

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func scheduleConfig(orgName, name, prompt, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_schedule" "test" {
  org_id = devin_organization.test.org_id
  name   = %q
  prompt = %q
%s}
`, providerConfig, orgName, name, prompt, extra)
}

// Create a recurring schedule with server-side defaults, validate against the
// API, import with the composite ID, update fields in place, then clear the
// optional tags again.
func TestAccScheduleResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-sched-org")
	var scheduleID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: scheduleConfig(orgName, "Initial schedule", "Run the nightly checks", "  frequency = \"0 9 * * 1\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_schedule.test", "schedule_id"),
					resource.TestCheckResourceAttr("devin_schedule.test", "name", "Initial schedule"),
					resource.TestCheckResourceAttr("devin_schedule.test", "prompt", "Run the nightly checks"),
					resource.TestCheckResourceAttr("devin_schedule.test", "frequency", "0 9 * * 1"),
					resource.TestCheckResourceAttr("devin_schedule.test", "schedule_type", "recurring"),
					resource.TestCheckResourceAttr("devin_schedule.test", "interval_count", "1"),
					resource.TestCheckResourceAttr("devin_schedule.test", "enabled", "true"),
					resource.TestCheckResourceAttr("devin_schedule.test", "notify_on", "failure"),
					resource.TestCheckResourceAttr("devin_schedule.test", "agent", "devin"),
					resource.TestCheckResourceAttr("devin_schedule.test", "bypass_approval", "false"),
					resource.TestCheckResourceAttrPair("devin_schedule.test", "org_id", "devin_organization.test", "org_id"),
					checkAPIFieldMatchesState("devin_schedule.test", "/v3/organizations/{org_id}/schedules/{schedule_id}", "name", "name"),
					checkAPIFieldMatchesState("devin_schedule.test", "/v3/organizations/{org_id}/schedules/{schedule_id}", "prompt", "prompt"),
					checkAPIFieldMatchesState("devin_schedule.test", "/v3/organizations/{org_id}/schedules/{schedule_id}", "frequency", "frequency"),
					captureStateAttr("devin_schedule.test", "schedule_id", &scheduleID),
				),
			},
			{
				ResourceName:                         "devin_schedule.test",
				ImportState:                          true,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "schedule_id",
				ImportStateIdFunc:                    importStateIDFromAttrs("devin_schedule.test", "org_id", "schedule_id"),
			},
			{
				// In-place update of name/prompt/frequency plus optional fields.
				Config: scheduleConfig(orgName, "Updated schedule", "Run the weekly checks",
					"  frequency = \"0 6 * * 2\"\n  notify_on = \"always\"\n  enabled   = false\n  tags      = [\"tf-acc\"]\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_schedule.test", "name", "Updated schedule"),
					resource.TestCheckResourceAttr("devin_schedule.test", "prompt", "Run the weekly checks"),
					resource.TestCheckResourceAttr("devin_schedule.test", "frequency", "0 6 * * 2"),
					resource.TestCheckResourceAttr("devin_schedule.test", "notify_on", "always"),
					resource.TestCheckResourceAttr("devin_schedule.test", "enabled", "false"),
					resource.TestCheckResourceAttr("devin_schedule.test", "tags.#", "1"),
					resource.TestCheckResourceAttr("devin_schedule.test", "tags.0", "tf-acc"),
					resource.TestCheckResourceAttrPtr("devin_schedule.test", "schedule_id", &scheduleID),
					checkAPIFieldMatchesState("devin_schedule.test", "/v3/organizations/{org_id}/schedules/{schedule_id}", "name", "name"),
				),
			},
			{
				// Removing tags from config must clear them on the API side.
				Config: scheduleConfig(orgName, "Updated schedule", "Run the weekly checks",
					"  frequency = \"0 6 * * 2\"\n  notify_on = \"always\"\n  enabled   = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_schedule.test", "tags.#"),
					resource.TestCheckResourceAttrPtr("devin_schedule.test", "schedule_id", &scheduleID),
				),
			},
		},
	})
}

// A schedule can reference a playbook and clear it again.
func TestAccScheduleResource_playbook(t *testing.T) {
	orgName := randomName("tf-acc-sched-pb")
	var scheduleID string

	configWithPlaybook := fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_playbook" "test" {
  org_id = devin_organization.test.org_id
  title  = "Schedule playbook"
  body   = "Playbook used by the schedule acceptance test."
}

resource "devin_schedule" "test" {
  org_id      = devin_organization.test.org_id
  name        = "Playbook schedule"
  prompt      = "Follow the playbook"
  frequency   = "0 9 * * 1"
  playbook_id = devin_playbook.test.playbook_id
}
`, providerConfig, orgName)

	configWithoutPlaybook := fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_playbook" "test" {
  org_id = devin_organization.test.org_id
  title  = "Schedule playbook"
  body   = "Playbook used by the schedule acceptance test."
}

resource "devin_schedule" "test" {
  org_id    = devin_organization.test.org_id
  name      = "Playbook schedule"
  prompt    = "Follow the playbook"
  frequency = "0 9 * * 1"
}
`, providerConfig, orgName)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: configWithPlaybook,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("devin_schedule.test", "playbook_id", "devin_playbook.test", "playbook_id"),
					captureStateAttr("devin_schedule.test", "schedule_id", &scheduleID),
				),
			},
			{
				// Removing playbook_id from config must clear it on the API side.
				Config: configWithoutPlaybook,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("devin_schedule.test", "playbook_id"),
					resource.TestCheckResourceAttrPtr("devin_schedule.test", "schedule_id", &scheduleID),
				),
			},
		},
	})
}

// One-time schedules require a future scheduled_at instead of a frequency.
func TestAccScheduleResource_oneTime(t *testing.T) {
	orgName := randomName("tf-acc-sched-once")
	scheduledAt := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second).Format(time.RFC3339)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: scheduleConfig(orgName, "One-time schedule", "Run once",
					fmt.Sprintf("  schedule_type = \"one_time\"\n  scheduled_at  = %q\n", scheduledAt)),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_schedule.test", "schedule_id"),
					resource.TestCheckResourceAttr("devin_schedule.test", "schedule_type", "one_time"),
					resource.TestCheckResourceAttrSet("devin_schedule.test", "scheduled_at"),
					resource.TestCheckNoResourceAttr("devin_schedule.test", "frequency"),
				),
			},
		},
	})
}

// The API rejects invalid cron expressions.
func TestAccScheduleResource_invalidCron(t *testing.T) {
	orgName := randomName("tf-acc-sched-badcron")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      scheduleConfig(orgName, "Bad cron", "prompt", "  frequency = \"not-a-cron\"\n"),
				ExpectError: regexp.MustCompile(`(?i)cron|frequency`),
			},
		},
	})
}

// Cross-field rules are enforced at plan time by ValidateConfig: recurring
// schedules require frequency and forbid target_devin_id; one_time schedules
// require scheduled_at.
func TestAccScheduleResource_validateConfig(t *testing.T) {
	orgName := randomName("tf-acc-sched-validate")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      scheduleConfig(orgName, "No frequency", "prompt", ""),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`frequency is required for recurring schedules`),
			},
			{
				Config:      scheduleConfig(orgName, "Target on recurring", "prompt", "  frequency       = \"0 9 * * 1\"\n  target_devin_id = \"devin-123\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`target_devin_id is only allowed when schedule_type is "one_time"`),
			},
			{
				Config:      scheduleConfig(orgName, "One-time without time", "prompt", "  schedule_type = \"one_time\"\n"),
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`scheduled_at is required when schedule_type is "one_time"`),
			},
		},
	})
}

// Malformed composite import IDs must be rejected.
func TestAccScheduleResource_importInvalidID(t *testing.T) {
	orgName := randomName("tf-acc-sched-import")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: scheduleConfig(orgName, "Import test", "prompt", "  frequency = \"0 9 * * 1\"\n"),
			},
			{
				ResourceName:  "devin_schedule.test",
				ImportState:   true,
				ImportStateId: "missing-a-slash",
				ExpectError:   regexp.MustCompile(`Unexpected import identifier`),
			},
		},
	})
}

// A schedule deleted out-of-band must plan a recreate, not error.
func TestAccScheduleResource_outOfBandDelete(t *testing.T) {
	orgName := randomName("tf-acc-sched-oob")
	var orgID, scheduleID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: scheduleConfig(orgName, "OOB delete", "prompt", "  frequency = \"0 9 * * 1\"\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_schedule.test", "org_id", &orgID),
					captureStateAttr("devin_schedule.test", "schedule_id", &scheduleID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/schedules/%s", orgID, scheduleID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band schedule delete returned %d", status)
					}
				},
				Config:             scheduleConfig(orgName, "OOB delete", "prompt", "  frequency = \"0 9 * * 1\"\n"),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}
