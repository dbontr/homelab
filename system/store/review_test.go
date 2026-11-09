package store

import (
    "strings"
    "testing"
    "time"
)

func TestReviewTasksFlagsOnlyAgedUndatedActionableWork(t *testing.T) {
    s, _ := testStore(t)
    now := time.Date(2026, time.November, 9, 12, 0, 0, 0, s.zone)
    add := func(input TaskInput) Task {
        t.Helper()
        task, err := s.AddTask(input)
        if err != nil {
            t.Fatal(err)
        }
        return task
    }

    aged := add(TaskInput{Title: "Undated project", Minutes: 40})
    fresh := add(TaskInput{Title: "Recent project", Minutes: 40})
    dated := add(TaskInput{Title: "Future deadline", Due: "2027-02-01", Minutes: 40})
    scheduled := add(TaskInput{Title: "Scheduled work", Scheduled: "2026-11-17T11:00:00-06:00", Minutes: 40})
    parent := add(TaskInput{Title: "Decomposed parent", Minutes: 40})
    estimate := add(TaskInput{Title: "Needs duration"})
    prerequisite := add(TaskInput{Title: "Prerequisite", Minutes: 20})
    blocked := add(TaskInput{Title: "Blocked work", Minutes: 40, DependsOn: []string{prerequisite.ID}})

    s.mu.Lock()
    for id, task := range s.data.Tasks {
        task.CreatedAt = now
        if id == aged.ID || id == dated.ID || id == scheduled.ID || id == parent.ID ||
            id == estimate.ID || id == blocked.ID {
            task.CreatedAt = now.AddDate(0, 0, -15)
        }
        if id == parent.ID {
            task.Decomposed = true
        }
        s.data.Tasks[id] = task
    }
    s.mu.Unlock()

    review, err := s.ReviewTasks(now, 7)
    if err != nil {
        t.Fatal(err)
    }
    found := make(map[string]TaskReviewEntry)
    for _, entry := range review.Entries {
        found[entry.TaskID] = entry
    }
    if entry, ok := found[aged.ID]; !ok || entry.Category != "review" ||
        entry.OpenSince != "2026-10-25" {
        t.Fatalf("aged undated task: %+v, present=%t", entry, ok)
    }
    if entry := found[estimate.ID]; entry.Category != "needs_estimate" {
        t.Fatalf("missing estimate was not preserved: %+v", entry)
    }
    if entry := found[blocked.ID]; entry.Category != "blocked" || entry.DependencyCount != 1 {
        t.Fatalf("prerequisite was not preserved: %+v", entry)
    }
    for _, id := range []string{fresh.ID, dated.ID, scheduled.ID, parent.ID, prerequisite.ID} {
        if entry, found := found[id]; found {
            t.Fatalf("task %s should not require review: %+v", id, entry)
        }
    }
    if !strings.Contains(review.Note, "does not establish inactivity") {
        t.Fatalf("review must disclose the age-only signal: %s", review.Note)
    }
    for _, task := range s.Tasks("") {
        if task.ID == aged.ID && task.Status != "open" {
            t.Fatal("review changed task completion state")
        }
    }
}

func TestReviewTasksUsesCalendarDaysAcrossDaylightSaving(t *testing.T) {
    s, _ := testStore(t)
    now := time.Date(2026, time.March, 7, 12, 0, 0, 0, s.zone)
    within, err := s.AddTask(TaskInput{
        Title: "Before two-day local boundary", Due: "2026-03-09T11:30:00-05:00", Minutes: 30,
    })
    if err != nil {
        t.Fatal(err)
    }
    beyond, err := s.AddTask(TaskInput{
        Title: "After two-day local boundary", Due: "2026-03-09T12:30:00-05:00", Minutes: 30,
    })
    if err != nil {
        t.Fatal(err)
    }
    s.mu.Lock()
    for id, task := range s.data.Tasks {
        task.CreatedAt = now
        s.data.Tasks[id] = task
    }
    s.mu.Unlock()

    review, err := s.ReviewTasks(now, 2)
    if err != nil {
        t.Fatal(err)
    }
    if len(review.Entries) != 1 || review.Entries[0].TaskID != within.ID ||
        review.Entries[0].Category != "upcoming" {
        t.Fatalf("local calendar-day deadline classification: %+v", review.Entries)
    }
    if review.Entries[0].TaskID == beyond.ID {
        t.Fatal("deadline outside the local two-day horizon was included")
    }
}

func TestAgendaConvertsTimestampDeadlinesToLocalDates(t *testing.T) {
    s, _ := testStore(t)
    earlier, err := s.AddTask(TaskInput{
        Title: "Local November ninth deadline", Due: "2026-11-10T00:15:00Z", Minutes: 20,
    })
    if err != nil {
        t.Fatal(err)
    }
    later, err := s.AddTask(TaskInput{
        Title: "Local November tenth deadline", Due: "2026-11-10T08:00:00Z", Minutes: 20,
    })
    if err != nil {
        t.Fatal(err)
    }
    rows, err := s.Agenda("2026-11-09")
    if err != nil {
        t.Fatal(err)
    }
    if len(rows) != 1 || rows[0].ID != earlier.ID {
        t.Fatalf("agenda included the wrong local deadline; later=%s rows=%+v", later.ID, rows)
    }
}

func TestTaskDeadlineUsesLocalMidnightOnDSTChanges(t *testing.T) {
    s, _ := testStore(t)
    for _, tt := range []struct {
        due string
        deadline string
    }{
        {"2026-03-08", "2026-03-09T00:00:00-05:00"},
        {"2026-11-01", "2026-11-02T00:00:00-06:00"},
        {"2026-11-09T23:15:00Z", "2026-11-09T23:15:00Z"},
    } {
        got, err := taskDeadline(tt.due, s.zone)
        if err != nil {
            t.Fatalf("deadline %q: %v", tt.due, err)
        }
        if got.Format(time.RFC3339) != tt.deadline {
            t.Fatalf("deadline %q = %s, want %s", tt.due, got.Format(time.RFC3339), tt.deadline)
        }
    }
}

func TestSuggestPrioritizesRecentlyOverdueTasks(t *testing.T) {
    s, _ := testStore(t)
    now := time.Now().UTC()
    _, err := s.AddTask(TaskInput{
        Title: "Due later today", Minutes: 20, Priority: 3,
        Due: now.Add(5 * time.Hour).Format(time.RFC3339),
    })
    if err != nil {
        t.Fatal(err)
    }
    overdue, err := s.AddTask(TaskInput{
        Title: "Past exact deadline", Minutes: 20, Priority: 3,
        Due: now.Add(-5 * time.Minute).Format(time.RFC3339),
    })
    if err != nil {
        t.Fatal(err)
    }
    suggested := s.Suggest(30, "", "", "", 2)
    if len(suggested) != 2 || suggested[0].ID != overdue.ID {
        t.Fatalf("overdue task must outrank a future task: %+v", suggested)
    }
}
