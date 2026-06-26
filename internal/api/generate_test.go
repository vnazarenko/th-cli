package api

import (
	"encoding/json"
	"reflect"
	"testing"
)

// These tests pin the *generated* types (client.gen.go) to the shapes the CLI
// relies on: top_profiles is a typed array we consume field-by-field, while a
// report is a loose object that must survive a raw-JSON round-trip with its
// real status nested at report.status (never a generated top-level field).

// topProfilesFixture is a trimmed two-item top_profiles response. It exercises a
// required field (pk), a present nullable (general_er) and an absent nullable
// (full_name on the second item) to prove the generated pointer fields behave.
const topProfilesFixture = `[
  {
    "username": "cristiano",
    "pk": "173560420",
    "country": "PT",
    "full_name": "Cristiano Ronaldo",
    "is_verified": true,
    "general_er": 1.23,
    "follower_count": 600000000,
    "media_count": 3500,
    "profile_pic_url": "https://example.com/cr7.jpg"
  },
  {
    "username": "leomessi",
    "pk": "427553890",
    "follower_count": 500000000
  }
]`

// reportFixture mirrors the real wrapper: the meaningful status is nested under
// `report.status`, with sibling sections each carrying their own status. There
// is deliberately NO top-level `status` key.
const reportFixture = `{
  "type": "full",
  "demo": false,
  "preview": {"status": "ready"},
  "report": {"status": "ready", "username": "cristiano", "followers": 600000000},
  "saves_shares_report": {"status": "collecting"},
  "cache": {"status": "ready"}
}`

func TestTopProfilesFixtureUnmarshalsIntoTypedArray(t *testing.T) {
	var profiles []TopProfile
	if err := json.Unmarshal([]byte(topProfilesFixture), &profiles); err != nil {
		t.Fatalf("top_profiles fixture must unmarshal into []TopProfile: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("len(profiles) = %d, want 2", len(profiles))
	}

	// Required scalar fields are plain (non-pointer) on the generated struct.
	if profiles[0].Username != "cristiano" {
		t.Errorf("profiles[0].Username = %q, want cristiano", profiles[0].Username)
	}
	if profiles[0].Pk != "173560420" {
		t.Errorf("profiles[0].Pk = %q, want 173560420", profiles[0].Pk)
	}

	// Present nullable fields are decoded into non-nil pointers.
	if profiles[0].GeneralEr == nil || *profiles[0].GeneralEr != 1.23 {
		t.Errorf("profiles[0].GeneralEr = %v, want 1.23", profiles[0].GeneralEr)
	}
	if profiles[0].FollowerCount == nil || *profiles[0].FollowerCount != 600000000 {
		t.Errorf("profiles[0].FollowerCount = %v, want 600000000", profiles[0].FollowerCount)
	}
	if profiles[0].IsVerified == nil || !*profiles[0].IsVerified {
		t.Errorf("profiles[0].IsVerified = %v, want true", profiles[0].IsVerified)
	}

	// Absent nullable fields stay nil rather than zero-valued.
	if profiles[1].FullName != nil {
		t.Errorf("profiles[1].FullName = %v, want nil (omitted)", profiles[1].FullName)
	}
	if profiles[1].GeneralEr != nil {
		t.Errorf("profiles[1].GeneralEr = %v, want nil (omitted)", profiles[1].GeneralEr)
	}
}

func TestReportFixtureRoundTripsThroughLooseObject(t *testing.T) {
	// The generated ReportResponse is a free-form map (no typed fields), so the
	// blob round-trips losslessly through it.
	var report ReportResponse
	if err := json.Unmarshal([]byte(reportFixture), &report); err != nil {
		t.Fatalf("report fixture must unmarshal into ReportResponse: %v", err)
	}

	// There is no generated top-level status field; the loose map must NOT have
	// invented one, and the real status lives nested at report.status.
	if _, ok := report["status"]; ok {
		t.Errorf("ReportResponse should have no top-level status key; got %v", report["status"])
	}
	nested, ok := report["report"].(map[string]interface{})
	if !ok {
		t.Fatalf("report[\"report\"] = %T, want nested object", report["report"])
	}
	if nested["status"] != "ready" {
		t.Errorf("report.status = %v, want ready", nested["status"])
	}

	// Re-marshalling and decoding both forms into generic maps proves the loose
	// object preserved the payload exactly (key order aside).
	out, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("re-marshal ReportResponse: %v", err)
	}
	var want, got map[string]interface{}
	if err := json.Unmarshal([]byte(reportFixture), &want); err != nil {
		t.Fatalf("decode original fixture: %v", err)
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode round-tripped report: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Errorf("report did not round-trip losslessly:\n want %v\n  got %v", want, got)
	}
}
