package store

import (
	"testing"
	"time"
)

func TestWeeklyPlanOrdersUnfinishedPrerequisitesBeforeDependents(t *testing.T) {
	s, _ := testStore(t)
	parent, err := s.AddTask(TaskInput{Title: "Derive theorem", Minutes: 60, Priority: 1})
	if err != nil { t.Fatal(err) }
	child, err := s.AddTask(TaskInput{Title: "Write proof", Minutes: 45, Priority: 5,
		Due: "2032-06-08", DependsOn: []string{parent.ID}})
	if err != nil { t.Fatal(err) }
	plan, err := s.PlanWeek("2032-06-07", 180, 9, nil)
	if err != nil { t.Fatal(err) }
	var first, second time.Time
	for _, slot := range plan.Slots {
		switch slot.TaskID {
		case parent.ID:
			first, _ = time.Parse(time.RFC3339, slot.End)
		case child.ID:
			second, _ = time.Parse(time.RFC3339, slot.Start)
		}
	}
	if first.IsZero() || second.IsZero() || second.Before(first) {
		t.Fatalf("dependency was not scheduled first: %+v", plan)
	}
	if len(plan.Limitations) == 0 {
		t.Fatal("advisory dependency assumption was concealed")
	}
}

func TestWeeklyPlanRespectsUnevenDailyCapacity(t *testing.T) {
	s, _ := testStore(t)
	task, err := s.AddTask(TaskInput{Title: "Analyze experiment", Minutes: 60})
	if err != nil { t.Fatal(err) }
	plan, err := s.PlanWeekWithCapacities("2032-06-07",
		[7]int{0, 90, 0, 0, 0, 0, 0}, 9, nil)
	if err != nil { t.Fatal(err) }
	if plan.DailyMinutes != 0 || len(plan.Slots) != 1 ||
		plan.Slots[0].TaskID != task.ID || plan.Slots[0].Date != "2032-06-08" {
		t.Fatalf("closed days were not respected: %+v", plan)
	}
	closed, err := s.PlanWeekWithCapacities("2032-06-07", [7]int{}, 9, nil)
	if err != nil || len(closed.Slots) != 0 || len(closed.Unplanned) != 1 {
		t.Fatalf("zero-capacity week was not represented as unavailable: %+v %v", closed, err)
	}
	for _, invalid := range [][7]int{{-1, 60, 60, 60, 60, 60, 60},
		{29, 60, 60, 60, 60, 60, 60}} {
		if _, err := s.PlanWeekWithCapacities("2032-06-07", invalid, 9, nil); err == nil {
			t.Fatalf("invalid capacities accepted: %v", invalid)
		}
	}
}

func TestWeeklyPlanLeavesUnplannablePrerequisiteUnscheduled(t *testing.T) {
	s, _ := testStore(t)
	parent, err := s.AddTask(TaskInput{Title: "Long experiment", Minutes: 300})
	if err != nil { t.Fatal(err) }
	child, err := s.AddTask(TaskInput{Title: "Interpret experiment", Minutes: 30, DependsOn: []string{parent.ID}})
	if err != nil { t.Fatal(err) }
	plan, err := s.PlanWeek("2032-06-07", 60, 9, nil)
	if err != nil { t.Fatal(err) }
	for _, slot := range plan.Slots {
		if slot.TaskID == child.ID {
			t.Fatal("dependent task bypassed an unplannable prerequisite")
		}
	}
	if len(plan.Unplanned) != 2 {
		t.Fatalf("unplannable dependency disappeared: %+v", plan)
	}
}

func TestWeeklyPlanScheduledPrerequisiteRemainsAdvisory(t *testing.T) {
	s, _ := testStore(t)
	parent, err := s.AddTask(TaskInput{
		Title: "Run apparatus", Minutes: 60, Scheduled: "2032-06-08T11:00:00Z",
	})
	if err != nil { t.Fatal(err) }
	child, err := s.AddTask(TaskInput{Title: "Analyze results", Minutes: 30, DependsOn: []string{parent.ID}})
	if err != nil { t.Fatal(err) }
	plan, err := s.PlanWeekWithCapacities("2032-06-07", [7]int{0, 180, 0, 0, 0, 0, 0}, 9, nil)
	if err != nil { t.Fatal(err) }
	if len(plan.Slots) != 1 || plan.Slots[0].TaskID != child.ID {
		t.Fatalf("scheduled prerequisite was not respected: %+v", plan)
	}
	start, parseErr := time.Parse(time.RFC3339, plan.Slots[0].Start)
	prerequisiteEnd, _ := time.Parse(time.RFC3339, "2032-06-08T12:00:00Z")
	if parseErr != nil || start.Before(prerequisiteEnd) {
		t.Fatalf("dependent task started before prerequisite finished: %+v", plan)
	}
	if len(plan.Limitations) == 0 {
		t.Fatal("scheduled prerequisite treated as confirmed completion")
	}
}
