package provider_test

import (
	"context"
	"fmt"
	"testing"

	sdkacctest "github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
	"github.com/jianyuan/terraform-provider-openai/internal/acctest"
	"github.com/openai/openai-go/v3"
)

// These tests need a sandbox organization: deleting a project's last external storage
// configuration switches the project back to organization-default retention. The mock server
// derives each validation outcome from the bucket name (see routes/external-storage.ts).

func TestAccExternalStorageResource_gcp(t *testing.T) {
	rn := "openai_external_storage.test"
	projectName := sdkacctest.RandomWithPrefix("tf-project")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckExternalStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccExternalStorageResourceGcpConfig(projectName, "tf-bucket"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("project_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("status"), knownvalue.StringExact("validated")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("geography"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("created_at"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("bucket"), knownvalue.StringExact("tf-bucket")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("workload_identity_project_number"), knownvalue.StringExact("123456789012")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("workload_identity_pool_id"), knownvalue.StringExact("openai-pool")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("workload_identity_provider_id"), knownvalue.StringExact("openai-provider")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("audience"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("region"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("aws"), knownvalue.Null()),
				},
			},
			{
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
			{
				Config: testAccExternalStorageResourceGcpConfig(projectName, "tf-bucket-changed"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction(rn, plancheck.ResourceActionDestroyBeforeCreate),
					},
				},
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp").AtMapKey("bucket"), knownvalue.StringExact("tf-bucket-changed")),
				},
			},
		},
	})
}

func TestAccExternalStorageResource_aws(t *testing.T) {
	rn := "openai_external_storage.test"
	projectName := sdkacctest.RandomWithPrefix("tf-project")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckExternalStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
resource "openai_project" "test" {
	name = %[1]q
}

resource "openai_external_storage" "test" {
	project_id = openai_project.test.id
	aws = {
		bucket   = "tf-bucket"
		role_arn = "arn:aws:iam::123456789012:role/openai-storage"
	}
}
`, projectName),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("status"), knownvalue.StringExact("validated")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("aws").AtMapKey("bucket"), knownvalue.StringExact("tf-bucket")),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("aws").AtMapKey("account_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("aws").AtMapKey("external_id"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("aws").AtMapKey("region"), knownvalue.NotNull()),
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("gcp"), knownvalue.Null()),
				},
			},
			{
				ResourceName:      rn,
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}

func TestAccExternalStorageResource_pendingValidation(t *testing.T) {
	rn := "openai_external_storage.test"
	projectName := sdkacctest.RandomWithPrefix("tf-project")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckExternalStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccExternalStorageResourceGcpConfig(projectName, "tf-bucket-pending"),
				ConfigStateChecks: []statecheck.StateCheck{
					statecheck.ExpectKnownValue(rn, tfjsonpath.New("status"), knownvalue.StringExact("validated")),
				},
			},
		},
	})
}

func TestAccExternalStorageResource_validationFailure(t *testing.T) {
	projectName := sdkacctest.RandomWithPrefix("tf-project")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckExternalStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccExternalStorageResourceGcpConfig(projectName, "tf-bucket-invalid"),
				ExpectError: acctest.ExpectLiteralError(`OpenAI could not write to tf-bucket-invalid: access denied.`),
			},
			{
				// The failed configuration must have been deleted, leaving the project without one.
				Config: fmt.Sprintf(`
resource "openai_project" "test" {
	name = %[1]q
}
`, projectName),
				Check: testAccCheckNoExternalStorageForProject("openai_project.test"),
			},
		},
	})
}

func TestAccExternalStorageResource_unhealthy(t *testing.T) {
	projectName := sdkacctest.RandomWithPrefix("tf-project")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { acctest.PreCheck(t) },
		ProtoV6ProviderFactories: acctest.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckExternalStorageDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccExternalStorageResourceGcpConfig(projectName, "tf-bucket-unhealthy"),
				ExpectError: acctest.ExpectLiteralError(`validation finished with status "unhealthy"`),
			},
		},
	})
}

func testAccExternalStorageResourceGcpConfig(projectName, bucket string) string {
	return fmt.Sprintf(`
resource "openai_project" "test" {
	name = %[1]q
}

resource "openai_external_storage" "test" {
	project_id = openai_project.test.id
	gcp = {
		bucket                           = %[2]q
		workload_identity_project_number = "123456789012"
		workload_identity_pool_id        = "openai-pool"
		workload_identity_provider_id    = "openai-provider"
	}
}
`, projectName, bucket)
}

func testAccCheckExternalStorageDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "openai_external_storage" {
			continue
		}

		_, err := acctest.SharedClient.Admin.Organization.ExternalStorage.Get(context.Background(), rs.Primary.ID)
		if err == nil {
			return fmt.Errorf("external storage %s still exists", rs.Primary.ID)
		}
	}
	return nil
}

func testAccCheckNoExternalStorageForProject(projectResourceName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[projectResourceName]
		if !ok {
			return fmt.Errorf("not found: %s", projectResourceName)
		}

		page, err := acctest.SharedClient.Admin.Organization.ExternalStorage.List(context.Background(), openai.AdminOrganizationExternalStorageListParams{
			ProjectID: openai.String(rs.Primary.ID),
		})
		if err != nil {
			return err
		}
		if len(page.Data) > 0 {
			return fmt.Errorf("expected no external storage for project %s, found %s", rs.Primary.ID, page.Data[0].ID)
		}
		return nil
	}
}
