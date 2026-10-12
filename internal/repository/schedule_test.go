package repository

import (
	"testing"
	"time"
)

func TestPureDateRendersCalendarDatesWithoutTime(t *testing.T) {
	stamp := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	cases := map[string]struct {
		value any
		want  string
	}{
		"time value":       {stamp, "2026-10-12"},
		"pure date string": {"2026-10-12", "2026-10-12"},
		"timestamp string": {"2026-10-12T00:00:00Z", "2026-10-12"},
		"byte slice":       {[]byte("2026-03-01T00:00:00Z"), "2026-03-01"},
		"empty string":     {"", ""},
		"nil":              {nil, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := PureDate(tc.value); got != tc.want {
				t.Fatalf("PureDate = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeScheduleRowsKeepsThePureDateContract(t *testing.T) {
	rows := []map[string]any{
		{"id": "s1", "planned_date": time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC), "planned_time": "19:30"},
		{"id": "s2", "planned_date": "2026-10-13T00:00:00Z", "planned_time": "12:30"},
	}
	NormalizeScheduleRows(rows)
	if rows[0]["planned_date"] != "2026-10-12" || rows[1]["planned_date"] != "2026-10-13" {
		t.Fatalf("unexpected dates: %#v", rows)
	}
	if rows[0]["planned_time"] != "19:30" {
		t.Fatal("normalization must not touch other columns")
	}
}

func TestParsePlannedDateAcceptsDatesAndTimestamps(t *testing.T) {
	cases := map[string]struct {
		input string
		want  string
		fail  bool
	}{
		"pure date":       {"2026-10-12", "2026-10-12", false},
		"timestamp":       {"2026-10-12T00:00:00Z", "2026-10-12", false},
		"datetime-local":  {"2026-10-12T19:30", "2026-10-12", false},
		"padded spaces":   {" 2026-10-12 ", "2026-10-12", false},
		"empty":           {"", "", true},
		"bad format":      {"12/10/2026", "", true},
		"impossible date": {"2026-02-30", "", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := ParsePlannedDate(tc.input)
			if tc.fail {
				if err == nil {
					t.Fatalf("ParsePlannedDate(%q) must fail", tc.input)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("ParsePlannedDate(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
