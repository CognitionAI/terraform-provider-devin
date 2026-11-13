package provider

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// apiKeyActive reports whether the API lists the key as active; a key the API
// does not list at all is an error.
func apiKeyActive(t *testing.T, serviceUserID, apiKeyID string) (bool, error) {
	status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3beta1/enterprise/service-users/"+serviceUserID+"/api-keys", nil)
	if status != http.StatusOK {
		return false, fmt.Errorf("listing API keys returned %d", status)
	}
	items, _ := parsed["items"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["api_key_id"] == apiKeyID {
			active, _ := item["is_active"].(bool)
			return active, nil
		}
	}
	return false, fmt.Errorf("API key %s is not listed for service user %s", apiKeyID, serviceUserID)
}

func checkAPIKeyActive(t *testing.T, serviceUserID string, apiKeyID *string, want bool) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		active, err := apiKeyActive(t, serviceUserID, *apiKeyID)
		if err != nil {
			return err
		}
		if active != want {
			return fmt.Errorf("API key %s active = %t, want %t", *apiKeyID, active, want)
		}
		return nil
	}
}

// Create a key and authenticate with its token, rename it (a replacement that
// creates the new key before revoking the old one), revoke it out of band
// (the next apply creates a new one), then verify destroy revokes the key.
func TestAccEnterpriseServiceUserAPIKeyResource_basic(t *testing.T) {
	testAccSetupPreCheck(t)
	suID := provisionEnterpriseServiceUser(t)
	const resourceName = "devin_enterprise_service_user_api_key.test"
	expiresAt := time.Now().Add(24 * time.Hour).Unix()
	firstName := randomName("tf-acc-key")
	secondName := randomName("tf-acc-key-renamed")
	var firstKeyID, secondKeyID, thirdKeyID string

	config := func(name string) string {
		return fmt.Sprintf(`%s
resource "devin_enterprise_service_user_api_key" "test" {
  service_user_id = %q
  name            = %q
  expires_at      = %d

  lifecycle {
    create_before_destroy = true
  }
}
`, providerConfig, suID, name, expiresAt)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config(firstName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", firstName),
					resource.TestCheckResourceAttr(resourceName, "expires_at", fmt.Sprint(expiresAt)),
					resource.TestCheckResourceAttrSet(resourceName, "created_at"),
					captureStateAttr(resourceName, "api_key_id", &firstKeyID),
					checkAPIKeyActive(t, suID, &firstKeyID, true),
					checkAPIKeyMatchesState(t, suID, resourceName),
					func(s *terraform.State) error {
						token, err := stateAttr(s, resourceName, "token")
						if err != nil {
							return err
						}
						status, parsed := testAccAPIRequestWithToken(t, http.MethodGet, "/v3/self", nil, token)
						if status != http.StatusOK {
							return fmt.Errorf("GET /v3/self with the new token returned %d", status)
						}
						if parsed["service_user_id"] != suID {
							return fmt.Errorf("the new token authenticated as %v, want %s", parsed["service_user_id"], suID)
						}
						return nil
					},
				),
			},
			{
				Config: config(secondName),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(resourceName, plancheck.ResourceActionCreateBeforeDestroy),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(resourceName, "name", secondName),
					captureStateAttr(resourceName, "api_key_id", &secondKeyID),
					checkAPIKeyActive(t, suID, &secondKeyID, true),
					checkAPIKeyActive(t, suID, &firstKeyID, false),
				),
			},
			{
				PreConfig: func() {
					status, _ := testAccAPIRequest(t, http.MethodDelete, "/v3beta1/enterprise/service-users/"+suID+"/api-keys/"+secondKeyID, nil)
					if status != http.StatusOK {
						t.Fatalf("revoking the API key out of band returned %d", status)
					}
				},
				Config: config(secondName),
				Check: resource.ComposeAggregateTestCheckFunc(
					captureStateAttr(resourceName, "api_key_id", &thirdKeyID),
					func(_ *terraform.State) error {
						if thirdKeyID == secondKeyID {
							return fmt.Errorf("the revoked API key %s was not replaced", secondKeyID)
						}
						return nil
					},
					checkAPIKeyActive(t, suID, &thirdKeyID, true),
				),
			},
		},
		CheckDestroy: func(_ *terraform.State) error {
			return checkAPIKeyActive(t, suID, &thirdKeyID, false)(nil)
		},
	})
}

// checkAPIKeyMatchesState asserts the key's name, expiry and creation time match
// what the API reports.
func checkAPIKeyMatchesState(t *testing.T, serviceUserID, resourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		apiKeyID, err := stateAttr(s, resourceName, "api_key_id")
		if err != nil {
			return err
		}
		status, parsed := testAccAPIRequest(t, http.MethodGet, "/v3beta1/enterprise/service-users/"+serviceUserID+"/api-keys", nil)
		if status != http.StatusOK {
			return fmt.Errorf("listing API keys returned %d", status)
		}
		items, _ := parsed["items"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["api_key_id"] != apiKeyID {
				continue
			}
			for apiField, attr := range map[string]string{"api_key_name": "name", "expires_at": "expires_at", "created_at": "created_at"} {
				want, err := stateAttr(s, resourceName, attr)
				if err != nil {
					return err
				}
				got := fmt.Sprint(item[apiField])
				if n, ok := item[apiField].(float64); ok {
					got = fmt.Sprint(int64(n))
				}
				if got != want {
					return fmt.Errorf("API %s = %s, state %s = %s", apiField, got, attr, want)
				}
			}
			return nil
		}
		return fmt.Errorf("API key %s is not listed", apiKeyID)
	}
}
