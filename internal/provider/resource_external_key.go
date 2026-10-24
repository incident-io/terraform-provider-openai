package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	supertypes "github.com/orange-cloudavenue/terraform-plugin-framework-supertypes"
)

// This resource is written by hand because openai-go has no external keys service yet, so it
// calls /organization/external_keys through the client's generic request methods. Once the SDK
// gains the service, it can move to providergen like the other resources.

var _ resource.Resource = &ExternalKeyResource{}
var _ resource.ResourceWithImportState = &ExternalKeyResource{}

func NewExternalKeyResource() resource.Resource {
	return &ExternalKeyResource{}
}

type ExternalKeyResource struct {
	baseResource
}

func (r *ExternalKeyResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_key"
}

func externalKeyProviderValidators() []validator.Object {
	return []validator.Object{
		objectvalidator.ExactlyOneOf(path.MatchRoot("aws"), path.MatchRoot("azure"), path.MatchRoot("gcp")),
	}
}

func externalKeyStringAttribute(description string) schema.StringAttribute {
	return schema.StringAttribute{
		MarkdownDescription: description,
		Required:            true,
		CustomType:          supertypes.StringType{},
	}
}

func (r *ExternalKeyResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Registers a customer-managed encryption key for Enterprise Key Management (EKM). Activate it on a project by setting the project's `external_key_id`. EKM is only available on certain accounts. Refer to the [EKM (External Keys) in the Management API](https://help.openai.com/en/articles/20000953-ekm-external-keys-in-the-management-api) article. Every argument forces a new resource, because the API does not support updates.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The ID of the external key, prefixed with `extkey_`.",
				Computed:            true,
				CustomType:          supertypes.StringType{},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "A friendly name for the external key.",
				Required:            true,
				CustomType:          supertypes.StringType{},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"aws": schema.SingleNestedAttribute{
				MarkdownDescription: "A key in AWS KMS. Exactly one of `aws`, `azure` or `gcp` must be set.",
				Optional:            true,
				CustomType:          supertypes.NewSingleNestedObjectTypeOf[ExternalKeyResourceModelAws](ctx),
				Validators:          externalKeyProviderValidators(),
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"role_arn":    externalKeyStringAttribute("The ARN of the IAM role OpenAI assumes in your account."),
					"kms_arn":     externalKeyStringAttribute("The ARN of the KMS key."),
					"external_id": externalKeyStringAttribute("The external ID OpenAI presents when assuming the role: your organization ID or project ID."),
				},
			},
			"azure": schema.SingleNestedAttribute{
				MarkdownDescription: "A key in Azure Key Vault. Exactly one of `aws`, `azure` or `gcp` must be set.",
				Optional:            true,
				CustomType:          supertypes.NewSingleNestedObjectTypeOf[ExternalKeyResourceModelAzure](ctx),
				Validators:          externalKeyProviderValidators(),
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"tenant_id": externalKeyStringAttribute("The Microsoft Entra tenant ID."),
					"vault_uri": externalKeyStringAttribute("The URI of the Key Vault that holds the key."),
					"key_name":  externalKeyStringAttribute("The name of the key in the Key Vault."),
				},
			},
			"gcp": schema.SingleNestedAttribute{
				MarkdownDescription: "A key in Google Cloud KMS, accessed through Workload Identity Federation. Exactly one of `aws`, `azure` or `gcp` must be set.",
				Optional:            true,
				CustomType:          supertypes.NewSingleNestedObjectTypeOf[ExternalKeyResourceModelGcp](ctx),
				Validators:          externalKeyProviderValidators(),
				PlanModifiers: []planmodifier.Object{
					objectplanmodifier.RequiresReplace(),
				},
				Attributes: map[string]schema.Attribute{
					"workload_identity_project_number": externalKeyStringAttribute("The number of the Google Cloud project that holds the Workload Identity pool."),
					"workload_identity_pool_id":        externalKeyStringAttribute("The ID of the Workload Identity pool."),
					"workload_identity_provider_id":    externalKeyStringAttribute("The ID of the Workload Identity provider registered for OpenAI."),
					"audience":                         externalKeyStringAttribute("The audience OpenAI uses when exchanging tokens with the Workload Identity provider. It must be one of the provider's allowed audiences."),
					"kms_project_id":                   externalKeyStringAttribute("The ID of the Google Cloud project that holds the key ring."),
					"kms_key_ring_name":                externalKeyStringAttribute("The name of the key ring."),
					"kms_key_name":                     externalKeyStringAttribute("The name of the key."),
					"kms_key_location":                 externalKeyStringAttribute("The location of the key ring, such as `us` or `europe-west1`."),
				},
			},
			"api_project_ids": schema.SetAttribute{
				MarkdownDescription: "The IDs of the projects that use this key.",
				Computed:            true,
				CustomType:          supertypes.NewSetTypeOf[string](ctx),
			},
			"created_at": schema.Int64Attribute{
				MarkdownDescription: "The Unix timestamp (in seconds) of when the key was registered.",
				Computed:            true,
				CustomType:          supertypes.Int64Type{},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *ExternalKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data ExternalKeyResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := r.getNewParams(ctx, data)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	var modelInstance *externalKey
	err := r.client.Post(ctx, "organization/external_keys", body, &modelInstance, externalKeyRequestOptions...)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create, got error: %s", clientErrorDetail(err)))
		return
	} else if modelInstance == nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create, got empty response body")
		return
	}

	resp.Diagnostics.Append(data.Fill(ctx, *modelInstance)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ExternalKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data ExternalKeyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	modelInstance, err := r.find(ctx, data.Id.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read, got error: %s", clientErrorDetail(err)))
		return
	} else if modelInstance == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(data.Fill(ctx, *modelInstance)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *ExternalKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Not Supported", "Update is not supported for this resource")
}

func (r *ExternalKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data ExternalKeyResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, "organization/external_keys/"+url.PathEscape(data.Id.ValueString()), nil, nil, externalKeyRequestOptions...)
	if err != nil {
		if apiErr, ok := errors.AsType[*openai.Error](err); ok && apiErr.StatusCode == http.StatusNotFound {
			return
		}

		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete, got error: %s", clientErrorDetail(err)))
		return
	}
}

func (r *ExternalKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// find pages through the organization's external keys and returns the one with the given ID,
// or nil if there is none. The API documents listing keys but not fetching one by ID.
func (r *ExternalKeyResource) find(ctx context.Context, id string) (*externalKey, error) {
	query := url.Values{"limit": {"100"}}
	for {
		var page externalKeyList
		if err := r.client.Get(ctx, "organization/external_keys?"+query.Encode(), nil, &page, externalKeyRequestOptions...); err != nil {
			return nil, err
		}

		for _, key := range page.Data {
			if key.ID == id {
				return &key, nil
			}
		}

		if !page.HasMore || page.LastID == "" {
			return nil, nil
		}
		query.Set("after", page.LastID)
	}
}

// The generic request methods prefer a project API key when one is configured, for example
// through OPENAI_API_KEY. Clearing it makes these admin-only requests use the admin key, as the
// SDK's admin services do.
var externalKeyRequestOptions = []option.RequestOption{option.WithAPIKey("")}

type ExternalKeyResourceModel struct {
	Id            supertypes.StringValue                                              `tfsdk:"id"`
	Name          supertypes.StringValue                                              `tfsdk:"name"`
	Aws           supertypes.SingleNestedObjectValueOf[ExternalKeyResourceModelAws]   `tfsdk:"aws"`
	Azure         supertypes.SingleNestedObjectValueOf[ExternalKeyResourceModelAzure] `tfsdk:"azure"`
	Gcp           supertypes.SingleNestedObjectValueOf[ExternalKeyResourceModelGcp]   `tfsdk:"gcp"`
	ApiProjectIds supertypes.SetValueOf[string]                                       `tfsdk:"api_project_ids"`
	CreatedAt     supertypes.Int64Value                                               `tfsdk:"created_at"`
}

type ExternalKeyResourceModelAws struct {
	RoleArn    supertypes.StringValue `tfsdk:"role_arn"`
	KmsArn     supertypes.StringValue `tfsdk:"kms_arn"`
	ExternalId supertypes.StringValue `tfsdk:"external_id"`
}

type ExternalKeyResourceModelAzure struct {
	TenantId supertypes.StringValue `tfsdk:"tenant_id"`
	VaultUri supertypes.StringValue `tfsdk:"vault_uri"`
	KeyName  supertypes.StringValue `tfsdk:"key_name"`
}

type ExternalKeyResourceModelGcp struct {
	WorkloadIdentityProjectNumber supertypes.StringValue `tfsdk:"workload_identity_project_number"`
	WorkloadIdentityPoolId        supertypes.StringValue `tfsdk:"workload_identity_pool_id"`
	WorkloadIdentityProviderId    supertypes.StringValue `tfsdk:"workload_identity_provider_id"`
	Audience                      supertypes.StringValue `tfsdk:"audience"`
	KmsProjectId                  supertypes.StringValue `tfsdk:"kms_project_id"`
	KmsKeyRingName                supertypes.StringValue `tfsdk:"kms_key_ring_name"`
	KmsKeyName                    supertypes.StringValue `tfsdk:"kms_key_name"`
	KmsKeyLocation                supertypes.StringValue `tfsdk:"kms_key_location"`
}

// externalKey is an external key as the API returns it: the provider's fields sit beside the
// common ones, distinguished by type. Requests use the same flat shape.
type externalKey struct {
	ID            string   `json:"id,omitempty"`
	Type          string   `json:"type"`
	Name          string   `json:"name"`
	CreatedAt     int64    `json:"created_at,omitempty"`
	APIProjectIDs []string `json:"api_project_ids,omitempty"`

	// aws
	RoleArn    string `json:"role_arn,omitempty"`
	KmsArn     string `json:"kms_arn,omitempty"`
	ExternalID string `json:"external_id,omitempty"`

	// azure
	TenantID string `json:"tenant_id,omitempty"`
	VaultURI string `json:"vault_uri,omitempty"`
	KeyName  string `json:"key_name,omitempty"`

	// gcp
	WorkloadIdentityProjectNumber string `json:"workload_identity_project_number,omitempty"`
	WorkloadIdentityPoolID        string `json:"workload_identity_pool_id,omitempty"`
	WorkloadIdentityProviderID    string `json:"workload_identity_provider_id,omitempty"`
	Audience                      string `json:"audience,omitempty"`
	KmsProjectID                  string `json:"kms_project_id,omitempty"`
	KmsKeyRingName                string `json:"kms_key_ring_name,omitempty"`
	KmsKeyName                    string `json:"kms_key_name,omitempty"`
	KmsKeyLocation                string `json:"kms_key_location,omitempty"`
}

type externalKeyList struct {
	Data    []externalKey `json:"data"`
	HasMore bool          `json:"has_more"`
	LastID  string        `json:"last_id"`
}

func (r *ExternalKeyResource) getNewParams(ctx context.Context, data ExternalKeyResourceModel) (*externalKey, diag.Diagnostics) {
	var diags diag.Diagnostics
	params := &externalKey{
		Name: data.Name.ValueString(),
	}

	// The schema's ExactlyOneOf validators guarantee exactly one block is set.
	switch {
	case !data.Aws.IsNull():
		v, d := data.Aws.Get(ctx)
		if diags.Append(d...); diags.HasError() {
			return nil, diags
		}
		params.Type = "aws"
		params.RoleArn = v.RoleArn.ValueString()
		params.KmsArn = v.KmsArn.ValueString()
		params.ExternalID = v.ExternalId.ValueString()
	case !data.Azure.IsNull():
		v, d := data.Azure.Get(ctx)
		if diags.Append(d...); diags.HasError() {
			return nil, diags
		}
		params.Type = "azure"
		params.TenantID = v.TenantId.ValueString()
		params.VaultURI = v.VaultUri.ValueString()
		params.KeyName = v.KeyName.ValueString()
	case !data.Gcp.IsNull():
		v, d := data.Gcp.Get(ctx)
		if diags.Append(d...); diags.HasError() {
			return nil, diags
		}
		params.Type = "gcp"
		params.WorkloadIdentityProjectNumber = v.WorkloadIdentityProjectNumber.ValueString()
		params.WorkloadIdentityPoolID = v.WorkloadIdentityPoolId.ValueString()
		params.WorkloadIdentityProviderID = v.WorkloadIdentityProviderId.ValueString()
		params.Audience = v.Audience.ValueString()
		params.KmsProjectID = v.KmsProjectId.ValueString()
		params.KmsKeyRingName = v.KmsKeyRingName.ValueString()
		params.KmsKeyName = v.KmsKeyName.ValueString()
		params.KmsKeyLocation = v.KmsKeyLocation.ValueString()
	default:
		diags.AddError("Missing provider", "One of `aws`, `azure` or `gcp` must be set.")
	}

	return params, diags
}

func (m *ExternalKeyResourceModel) Fill(ctx context.Context, data externalKey) (diags diag.Diagnostics) {
	m.Id = supertypes.NewStringValue(data.ID)
	m.Name = supertypes.NewStringValue(data.Name)
	m.CreatedAt = supertypes.NewInt64Value(data.CreatedAt)
	m.ApiProjectIds = supertypes.NewSetValueOfSlice(ctx, data.APIProjectIDs)

	m.Aws = supertypes.NewSingleNestedObjectValueOfNull[ExternalKeyResourceModelAws](ctx)
	m.Azure = supertypes.NewSingleNestedObjectValueOfNull[ExternalKeyResourceModelAzure](ctx)
	m.Gcp = supertypes.NewSingleNestedObjectValueOfNull[ExternalKeyResourceModelGcp](ctx)

	switch data.Type {
	case "aws":
		m.Aws = supertypes.NewSingleNestedObjectValueOf(ctx, &ExternalKeyResourceModelAws{
			RoleArn:    supertypes.NewStringValue(data.RoleArn),
			KmsArn:     supertypes.NewStringValue(data.KmsArn),
			ExternalId: supertypes.NewStringValue(data.ExternalID),
		})
	case "azure":
		m.Azure = supertypes.NewSingleNestedObjectValueOf(ctx, &ExternalKeyResourceModelAzure{
			TenantId: supertypes.NewStringValue(data.TenantID),
			VaultUri: supertypes.NewStringValue(data.VaultURI),
			KeyName:  supertypes.NewStringValue(data.KeyName),
		})
	case "gcp":
		m.Gcp = supertypes.NewSingleNestedObjectValueOf(ctx, &ExternalKeyResourceModelGcp{
			WorkloadIdentityProjectNumber: supertypes.NewStringValue(data.WorkloadIdentityProjectNumber),
			WorkloadIdentityPoolId:        supertypes.NewStringValue(data.WorkloadIdentityPoolID),
			WorkloadIdentityProviderId:    supertypes.NewStringValue(data.WorkloadIdentityProviderID),
			Audience:                      supertypes.NewStringValue(data.Audience),
			KmsProjectId:                  supertypes.NewStringValue(data.KmsProjectID),
			KmsKeyRingName:                supertypes.NewStringValue(data.KmsKeyRingName),
			KmsKeyName:                    supertypes.NewStringValue(data.KmsKeyName),
			KmsKeyLocation:                supertypes.NewStringValue(data.KmsKeyLocation),
		})
	default:
		diags.AddError("Unsupported provider", fmt.Sprintf("External key %s uses provider type %q, which this version of the provider does not support.", data.ID, data.Type))
	}

	return
}
