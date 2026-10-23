package main

import (
	"fmt"
	"mime"
	"strings"
)

const maxAssetBytes int64 = 10 << 20

type uploadIntent struct {
	AppointmentID string `json:"appointment_id"`
	WorkflowState string `json:"workflow_state"`
	AssetKind     string `json:"asset_kind"`
	ContentType   string `json:"content_type"`
	SizeBytes     int64  `json:"size_bytes"`
}

type policyError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *policyError) Error() string { return e.Message }

func authorizeUpload(in uploadIntent) error {
	if strings.TrimSpace(in.AppointmentID) == "" {
		return &policyError{Field: "appointment_id", Message: "appointment_id is required"}
	}
	if in.WorkflowState != "scheduled" && in.WorkflowState != "checked_in" {
		return &policyError{Field: "workflow_state", Message: "uploads require a scheduled or checked-in appointment"}
	}
	allowed := map[string]map[string]bool{
		"referral": {"application/pdf": true},
		"photo":    {"image/jpeg": true, "image/png": true},
	}
	mediaType, _, err := mime.ParseMediaType(in.ContentType)
	if err != nil || !allowed[in.AssetKind][mediaType] {
		return &policyError{Field: "content_type", Message: "content type is not allowed for this asset kind"}
	}
	if in.SizeBytes <= 0 || in.SizeBytes > maxAssetBytes {
		return &policyError{Field: "size_bytes", Message: fmt.Sprintf("size_bytes must be between 1 and %d", maxAssetBytes)}
	}
	return nil
}
