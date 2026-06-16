package provider

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func secretConfig(orgName, key, value, extra string) string {
	return fmt.Sprintf(`%s
resource "devin_organization" "test" {
  name = %q
}

resource "devin_secret" "test" {
  org_id = devin_organization.test.org_id
  key    = %q
  value  = %q
%s}
`, providerConfig, orgName, key, value, extra)
}

// checkSecretExistsInAPI asserts that the secret in state is returned by the
// org secrets list endpoint (there is no GET-by-id endpoint).
func checkSecretExistsInAPI(t *testing.T, resourceName string, expectExists bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		orgID, err := stateAttr(s, resourceName, "org_id")
		if err != nil {
			return err
		}
		secretID, err := stateAttr(s, resourceName, "secret_id")
		if err != nil {
			return err
		}

		status, parsed := testAccAPIRequest(t, http.MethodGet, fmt.Sprintf("/v3/organizations/%s/secrets", orgID), nil)
		if status != http.StatusOK {
			return fmt.Errorf("listing secrets returned %d", status)
		}
		items, _ := parsed["items"].([]any)
		found := false
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if ok && item["secret_id"] == secretID {
				found = true
				break
			}
		}
		if found != expectExists {
			return fmt.Errorf("secret %s exists in API = %t, expected %t", secretID, found, expectExists)
		}
		return nil
	}
}

// Create with defaults, validate it exists via the API, then verify that
// changing any attribute plans a replacement (the API has no secret update
// endpoint).
func TestAccSecretResource_basic(t *testing.T) {
	orgName := randomName("tf-acc-secret-org")
	key := "TF_ACC_SECRET"

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: secretConfig(orgName, key, "initial-value", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_secret.test", "secret_id"),
					resource.TestCheckResourceAttr("devin_secret.test", "key", key),
					resource.TestCheckResourceAttr("devin_secret.test", "value", "initial-value"),
					resource.TestCheckResourceAttr("devin_secret.test", "type", "key-value"),
					resource.TestCheckResourceAttr("devin_secret.test", "is_sensitive", "true"),
					resource.TestCheckResourceAttrPair("devin_secret.test", "org_id", "devin_organization.test", "org_id"),
					checkSecretExistsInAPI(t, "devin_secret.test", true),
				),
			},
			{
				// Secrets cannot be updated in place; any change must replace.
				Config: secretConfig(orgName, key, "rotated-value", ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("devin_secret.test", plancheck.ResourceActionReplace),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_secret.test", "value", "rotated-value"),
					checkSecretExistsInAPI(t, "devin_secret.test", true),
				),
			},
		},
	})
}

// All optional attributes round-trip and the secret is removed from the API on
// destroy.
func TestAccSecretResource_allAttributes(t *testing.T) {
	orgName := randomName("tf-acc-secret-attrs")
	var orgID, secretID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: secretConfig(orgName, "TF_ACC_SECRET_FULL", "s3cret", "  type         = \"key-value\"\n  note         = \"created by acceptance tests\"\n  is_sensitive = false\n"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devin_secret.test", "note", "created by acceptance tests"),
					resource.TestCheckResourceAttr("devin_secret.test", "is_sensitive", "false"),
					captureStateAttr("devin_secret.test", "org_id", &orgID),
					captureStateAttr("devin_secret.test", "secret_id", &secretID),
				),
			},
		},
		CheckDestroy: func(s *terraform.State) error {
			status, parsed := testAccAPIRequest(t, http.MethodGet, fmt.Sprintf("/v3/organizations/%s/secrets", orgID), nil)
			// The org is destroyed along with the secret; a 404/403 for the org
			// also proves the secret is gone.
			if status != http.StatusOK {
				return nil
			}
			items, _ := parsed["items"].([]any)
			for _, raw := range items {
				item, ok := raw.(map[string]any)
				if ok && item["secret_id"] == secretID {
					return fmt.Errorf("secret %s still exists after destroy", secretID)
				}
			}
			return nil
		},
	})
}

// A secret deleted out-of-band must plan a recreate, not error.
func TestAccSecretResource_outOfBandDelete(t *testing.T) {
	orgName := randomName("tf-acc-secret-oob")
	var orgID, secretID string

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: secretConfig(orgName, "TF_ACC_SECRET_OOB", "value", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr("devin_secret.test", "org_id", &orgID),
					captureStateAttr("devin_secret.test", "secret_id", &secretID),
				),
			},
			{
				PreConfig: func() {
					path := fmt.Sprintf("/v3/organizations/%s/secrets/%s", orgID, secretID)
					status, _ := testAccAPIRequest(t, http.MethodDelete, path, nil)
					if status != http.StatusOK {
						t.Fatalf("out-of-band secret delete returned %d", status)
					}
				},
				Config:             secretConfig(orgName, "TF_ACC_SECRET_OOB", "value", ""),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
		},
	})
}

// Org-scoped tokens can manage secrets in their own org.
func TestAccSecretResource_orgToken(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccOrgTokenPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`%s
resource "devin_secret" "org_scoped" {
  org_id = %q
  key    = "TF_ACC_ORG_TOKEN_SECRET"
  value  = "org-scoped-value"
}
`, orgTokenProviderConfig(), os.Getenv("DEVIN_ORG_TOKEN_ORG_ID")),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devin_secret.org_scoped", "secret_id"),
					resource.TestCheckResourceAttr("devin_secret.org_scoped", "org_id", os.Getenv("DEVIN_ORG_TOKEN_ORG_ID")),
				),
			},
		},
	})
}
