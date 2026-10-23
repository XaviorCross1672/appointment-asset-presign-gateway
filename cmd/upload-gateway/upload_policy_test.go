package main

import "testing"

func TestAuthorizeUpload(t *testing.T) {
	tests := []struct {
		name    string
		intent  uploadIntent
		wantErr bool
	}{
		{
			name:   "scheduled referral PDF",
			intent: uploadIntent{AppointmentID: "apt_123", WorkflowState: "scheduled", AssetKind: "referral", ContentType: "application/pdf", SizeBytes: 2048},
		},
		{
			name:   "checked-in patient photo",
			intent: uploadIntent{AppointmentID: "apt_456", WorkflowState: "checked_in", AssetKind: "photo", ContentType: "image/jpeg", SizeBytes: 4096},
		},
		{
			name:    "completed appointment",
			intent:  uploadIntent{AppointmentID: "apt_789", WorkflowState: "completed", AssetKind: "referral", ContentType: "application/pdf", SizeBytes: 2048},
			wantErr: true,
		},
		{
			name:    "oversized photo",
			intent:  uploadIntent{AppointmentID: "apt_456", WorkflowState: "checked_in", AssetKind: "photo", ContentType: "image/png", SizeBytes: maxAssetBytes + 1},
			wantErr: true,
		},
		{
			name:    "executable content",
			intent:  uploadIntent{AppointmentID: "apt_123", WorkflowState: "scheduled", AssetKind: "referral", ContentType: "application/octet-stream", SizeBytes: 2048},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := authorizeUpload(test.intent)
			if (err != nil) != test.wantErr {
				t.Fatalf("authorizeUpload() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
