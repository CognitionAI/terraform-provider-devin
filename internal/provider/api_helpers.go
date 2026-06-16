package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timetypes/timetypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/oapi-codegen/nullable"
)

// Helpers for converting between Terraform plugin framework values and the
// generated API models in internal/api. Generated request models use
// nullable.Nullable for fields where the API distinguishes "omitted" (leave
// unchanged / use server default) from explicit JSON null (clear the value).

// nullableStringFrom returns an explicit null when the value is null or
// unknown, for fields where the API clears the value on null.
func nullableStringFrom(value types.String) nullable.Nullable[string] {
	if value.IsNull() || value.IsUnknown() {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(value.ValueString())
}

// optionalStringFrom leaves the field unspecified (omitted from the request)
// when the value is null or unknown.
func optionalStringFrom(value types.String) nullable.Nullable[string] {
	var result nullable.Nullable[string]
	if !value.IsNull() && !value.IsUnknown() {
		result.Set(value.ValueString())
	}
	return result
}

// optionalIntFrom leaves the field unspecified when the value is null or
// unknown.
func optionalIntFrom(value types.Int64) nullable.Nullable[int] {
	var result nullable.Nullable[int]
	if !value.IsNull() && !value.IsUnknown() {
		result.Set(int(value.ValueInt64()))
	}
	return result
}

// boolPtrFrom returns nil when the value is null or unknown, for optional
// non-nullable request fields.
func boolPtrFrom(value types.Bool) *bool {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := value.ValueBool()
	return &v
}

// intPtrFrom returns nil when the value is null or unknown, for optional
// non-nullable request fields.
func intPtrFrom(value types.Int64) *int {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := int(value.ValueInt64())
	return &v
}

func stringFromNullable(value nullable.Nullable[string]) types.String {
	if !value.IsSpecified() || value.IsNull() {
		return types.StringNull()
	}
	return types.StringValue(value.MustGet())
}

func int64FromNullable(value nullable.Nullable[int]) types.Int64 {
	if !value.IsSpecified() || value.IsNull() {
		return types.Int64Null()
	}
	return types.Int64Value(int64(value.MustGet()))
}

func rfc3339FromNullable(value nullable.Nullable[time.Time]) timetypes.RFC3339 {
	if !value.IsSpecified() || value.IsNull() {
		return timetypes.NewRFC3339Null()
	}
	return timetypes.NewRFC3339TimeValue(value.MustGet())
}

func setToStrings(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	var values []string
	diags.Append(set.ElementsAs(ctx, &values, false)...)
	if values == nil {
		// Keep the slice non-nil so request bodies marshal as [] rather than null.
		values = []string{}
	}
	return values
}
