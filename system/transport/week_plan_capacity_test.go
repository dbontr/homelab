package transport

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbontr/homelab-new/system/store"
)

func TestWeeklyPlannerDailyBudgetsAndAPIValidation(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "tasks.json"), "UTC")
	if err != nil { t.Fatal(err) }
	_, err = db.AddTask(store.TaskInput{Title: "Read material", Minutes: 60})
	if err != nil { t.Fatal(err) }
	s, err := New(db, strings.Repeat("t", 40), nil)
	if err != nil { t.Fatal(err) }
	h := s.Handler()
	requestURI := "/v1/planning/week?from=2032-06-07&daily_minutes=180&start_hour=9&day_minutes=0,90,0,0,0,0,0"
	got := request(h, "GET", requestURI, "", strings.Repeat("t", 40))
	if got.Code != 200 { t.Fatalf("planning failed: %d %s", got.Code, got.Body.String()) }
	var plan store.WeeklyPlan
	if err := json.Unmarshal(got.Body.Bytes(), &plan); err != nil { t.Fatal(err) }
	if len(plan.Slots) != 1 || plan.Slots[0].Date != "2032-06-08" ||
		plan.DailyCapacities != [7]int{0,90,0,0,0,0,0} {
		t.Fatalf("day-specific capacity not enforced: %+v", plan)
	}
	for _, invalid := range []string{
		"day_minutes=0,60",
		"day_minutes=0,20,0,0,0,0,0",
		"day_minutes=0,60,abc,0,0,0,0",
	} {
		got := request(h, "GET", "/v1/planning/week?from=2032-06-07&"+invalid,
			"", strings.Repeat("t", 40))
		if got.Code != 400 {
			t.Fatalf("invalid budget accepted: %s code=%d", invalid, got.Code)
		}
	}
	got = request(h,"GET",requestURI,"","")
	if got.Code != 401 { t.Fatalf("anonymous plan read accepted: %d", got.Code) }
}
