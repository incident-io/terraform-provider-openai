# Register a Google Cloud KMS key for Enterprise Key Management (EKM).
resource "openai_external_key" "example" {
  name = "gcp-ekm"

  gcp = {
    workload_identity_project_number = "123456789012"
    workload_identity_pool_id        = "openai-pool"
    workload_identity_provider_id    = "openai-provider"
    audience                         = "org-000000000000000000000000"
    kms_project_id                   = "my-kms-project"
    kms_key_ring_name                = "openai"
    kms_key_name                     = "openai-ekm"
    kms_key_location                 = "us"
  }
}

# Encrypt a project's data with the key.
resource "openai_project" "example" {
  name            = "Example Project"
  external_key_id = openai_external_key.example.id
}
