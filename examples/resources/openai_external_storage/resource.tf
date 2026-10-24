resource "openai_project" "example" {
  name = "Example Project"
}

# Store the project's data in a Cloud Storage bucket that OpenAI reaches through
# Workload Identity Federation. Apply fails, and nothing is left registered, unless
# OpenAI can validate access to the bucket.
resource "openai_external_storage" "example" {
  project_id = openai_project.example.id

  gcp = {
    bucket                           = "my-openai-data"
    workload_identity_project_number = "123456789012"
    workload_identity_pool_id        = "openai-pool"
    workload_identity_provider_id    = "openai-provider"
  }
}
