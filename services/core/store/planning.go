package store

import (
 "errors"
 "sort"
 "time"
)

type BusyWindow struct {
 Start time.Time
 End time.Time
 Source string
 Title string
}
type PlannedTask struct {
 TaskID string `json:"task_id"`
 Title string `json:"title"`
 Date string `json:"date"`
 Start string `json:"start"`
 End string `json:"end"`
 Minutes int `json:"minutes"`
 Priority int `json:"priority"`
 Due string `json:"due,omitempty"`
 DeadlineStatus string `json:"deadline_status,omitempty"`
}
type UnplannedTask struct {
 TaskID string `json:"task_id"`
 Title string `json:"title"`
 Reason string `json:"reason"`
}
type WeeklyPlan struct {
 From string `json:"from"`
 Until string `json:"until"`
 DailyMinutes int `json:"daily_minutes"`
 DailyCapacities [7]int `json:"daily_capacities"`
 StartHour int `json:"start_hour"`
 Slots []PlannedTask `json:"slots"`
 Unplanned []UnplannedTask `json:"unplanned"`
 Reservations int `json:"reservations"`
 Limitations []string `json:"limitations"`
}

func overlap(aStart,aEnd,bStart,bEnd time.Time)bool{
 return aStart.Before(bEnd)&&bStart.Before(aEnd)
}

func (s *Store) PlanningStart(day string)(time.Time,error){
 if day==""{day=time.Now().In(s.zone).Format("2006-01-02")}
 first,err:=time.ParseInLocation("2006-01-02",day,s.zone)
 if err!=nil{return time.Time{},errors.New("planning day must be YYYY-MM-DD")}
 return first,nil
}

// PlanWeek preserves the uniform-capacity contract for existing clients.
func (s *Store) PlanWeek(day string, dailyMinutes, startHour int, external []BusyWindow) (WeeklyPlan, error) {
	var capacities [7]int
	for i := range capacities {
		capacities[i] = dailyMinutes
	}
	return s.PlanWeekWithCapacities(day, capacities, startHour, external)
}

// PlanWeekWithCapacities constructs advisory timeboxes without modifying task
// or calendar records. Dependencies may be planned earlier in the same week;
// such plans remain assumptions rather than evidence of task completion.
func (s *Store) PlanWeekWithCapacities(day string, capacities [7]int, startHour int, external []BusyWindow) (WeeklyPlan, error) {
	if startHour < 0 || startHour > 16 {
		return WeeklyPlan{}, errors.New("start hour must be 0..16")
	}
	maxCapacity := 0
	uniform := true
	for i, minutes := range capacities {
		if minutes < 0 || minutes > 480 || minutes > 0 && minutes < 30 {
			return WeeklyPlan{}, errors.New("daily capacity must be zero or 30..480 minutes")
		}
		if i > 0 && minutes != capacities[0] {
			uniform = false
		}
		if minutes > maxCapacity {
			maxCapacity = minutes
		}
	}
	first, err := s.PlanningStart(day)
	if err != nil {
		return WeeklyPlan{}, err
	}
	uniformMinutes := 0
	if uniform {
		uniformMinutes = capacities[0]
	}
	plan := WeeklyPlan{
		From: first.Format("2006-01-02"),
		Until: first.AddDate(0, 0, 7).Format("2006-01-02"),
		DailyMinutes: uniformMinutes,
		DailyCapacities: capacities,
		StartHour: startHour,
		Slots: []PlannedTask{},
		Unplanned: []UnplannedTask{},
		Limitations: []string{},
	}
	tasks := s.Tasks("")
	if len(tasks) > 2000 {
		plan.Limitations = append(plan.Limitations, "Planning is limited to the first 2000 recorded tasks")
		tasks = tasks[:2000]
	}

	now := time.Now().In(s.zone)
	completed := make(map[string]bool, len(tasks))
	scheduledEnds := make(map[string]time.Time)
	busy := make([]BusyWindow, 0, len(external)+len(tasks))
	for _, task := range tasks {
		if task.Status == "completed" {
			completed[task.ID] = true
		}
		if task.Status != "open" || task.Decomposed || task.Scheduled == "" || task.Minutes < 1 {
			continue
		}
		start, err := time.Parse(time.RFC3339, task.Scheduled)
		if err != nil {
			continue
		}
		end := start.Add(time.Duration(task.Minutes)*time.Minute)
		busy = append(busy, BusyWindow{Start: start, End: end, Source: "task", Title: task.Title})
		if end.After(now) && end.After(first) && start.Before(first.AddDate(0, 0, 7)) {
			scheduledEnds[task.ID] = end
		}
	}
	for _, window := range external {
		if window.End.After(window.Start) {
			busy = append(busy, window)
		}
	}
	plan.Reservations = len(busy)
	sort.Slice(busy, func(i, j int) bool { return busy[i].Start.Before(busy[j].Start) })

	remaining := make(map[string]Task)
	for _, task := range tasks {
		if task.Status != "open" || task.Decomposed || task.Scheduled != "" {
			continue
		}
		switch {
		case task.Minutes < 1:
			plan.Unplanned = append(plan.Unplanned, UnplannedTask{TaskID: task.ID, Title: task.Title, Reason: "Estimated duration is missing"})
		case task.Minutes > maxCapacity:
			plan.Unplanned = append(plan.Unplanned, UnplannedTask{TaskID: task.ID, Title: task.Title, Reason: "Duration exceeds available daily capacity"})
		default:
			remaining[task.ID] = task
		}
	}

	ordered := make([]Task, 0, len(remaining))
	for _, task := range remaining {
		ordered = append(ordered, task)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.Due != b.Due {
			if a.Due == "" { return false }
			if b.Due == "" { return true }
			deadlineA, errA := taskDeadline(a.Due, s.zone)
			deadlineB, errB := taskDeadline(b.Due, s.zone)
			if errA == nil && errB == nil && !deadlineA.Equal(deadlineB) {
				return deadlineA.Before(deadlineB)
			}
		}
		if a.Priority != b.Priority { return a.Priority > b.Priority }
		if a.Minutes != b.Minutes { return a.Minutes < b.Minutes }
		if !a.CreatedAt.Equal(b.CreatedAt) { return a.CreatedAt.Before(b.CreatedAt) }
		return a.ID < b.ID
	})

	used := [7]int{}
	plannedEnds := make(map[string]time.Time)
	assumedDependencies := false
	lateProposals := false
	for pass := 0; pass <= len(ordered) && len(remaining) > 0; pass++ {
		progress := false
		for _, task := range ordered {
			if _, exists := remaining[task.ID]; !exists {
				continue
			}
			var earliest time.Time
			pendingPrerequisite := false
			missingPrerequisite := false
			assumed := false
			for _, dep := range task.DependsOn {
				if completed[dep] {
					continue
				}
				if end, exists := plannedEnds[dep]; exists {
					if end.After(earliest) { earliest = end }
					assumed = true
					continue
				}
				if end, exists := scheduledEnds[dep]; exists {
					if end.After(earliest) { earliest = end }
					assumed = true
					continue
				}
				if _, pending := remaining[dep]; pending {
					pendingPrerequisite = true
				} else {
					missingPrerequisite = true
				}
			}
			if pendingPrerequisite {
				continue
			}
			if missingPrerequisite {
				plan.Unplanned = append(plan.Unplanned, UnplannedTask{
					TaskID: task.ID, Title: task.Title, Reason: "Prerequisite is not completed or planned",
				})
				delete(remaining, task.ID)
				progress = true
				continue
			}

			duration := time.Duration(task.Minutes) * time.Minute
			deadline := time.Time{}
			if task.Due != "" {
				deadline, _ = taskDeadline(task.Due, s.zone)
			}
			placed := false
			for dayIndex := 0; dayIndex < 7 && !placed; dayIndex++ {
				if used[dayIndex]+task.Minutes > capacities[dayIndex] {
					continue
				}
				date := first.AddDate(0, 0, dayIndex)
				cursor := time.Date(date.Year(), date.Month(), date.Day(), startHour, 0, 0, 0, s.zone)
				cutoff := cursor.Add(8*time.Hour)
				if cursor.Before(now) {
					if !now.Before(cutoff) { continue }
					cursor = now
				}
				if earliest.After(cursor) {
					cursor = earliest
				}
				if cursor.Minute()%15 != 0 || cursor.Second() != 0 || cursor.Nanosecond() != 0 {
					cursor = cursor.Truncate(time.Minute).Add(time.Minute)
					cursor = cursor.Add(time.Duration((15-cursor.Minute()%15)%15)*time.Minute)
				}
				for move := 0; move < 100 && !cursor.Add(duration).After(cutoff); move++ {
					next := cursor.Add(duration)
					blocked := false
					for _, reserved := range busy {
						if overlap(cursor, next, reserved.Start, reserved.End) {
							if reserved.End.After(cursor) { cursor = reserved.End }
							blocked = true
							break
						}
					}
					if blocked { continue }
					deadlineStatus := ""
					if !deadline.IsZero() {
						if next.After(deadline) {
							deadlineStatus = "late"
							lateProposals = true
						} else {
							deadlineStatus = "on_time"
						}
					}
					plan.Slots = append(plan.Slots, PlannedTask{
						TaskID: task.ID, Title: task.Title, Date: date.Format("2006-01-02"),
						Start: cursor.Format(time.RFC3339), End: next.Format(time.RFC3339),
						Minutes: task.Minutes, Priority: task.Priority, Due: task.Due,
						DeadlineStatus: deadlineStatus,
					})
					busy = append(busy, BusyWindow{Start: cursor, End: next, Source: "proposal", Title: task.Title})
					used[dayIndex] += task.Minutes
					plannedEnds[task.ID] = next
					placed = true
					break
				}
			}
			if !placed {
				plan.Unplanned = append(plan.Unplanned, UnplannedTask{
					TaskID: task.ID, Title: task.Title, Reason: "No available slot within the requested capacity",
				})
			} else if assumed {
				assumedDependencies = true
			}
			delete(remaining, task.ID)
			progress = true
		}
		if !progress {
			break
		}
	}
	for _, task := range ordered {
		if _, exists := remaining[task.ID]; exists {
			plan.Unplanned = append(plan.Unplanned, UnplannedTask{
				TaskID: task.ID, Title: task.Title, Reason: "Prerequisite cannot be placed in this plan",
			})
		}
	}
	if assumedDependencies {
		plan.Limitations = append(plan.Limitations, "Dependent timeboxes assume earlier planned tasks will be completed; this is not confirmed completion")
	}
	if lateProposals {
		plan.Limitations = append(plan.Limitations, "Some proposed tasks finish after their due deadlines; these are late recovery timeboxes, not on-time completion plans")
	}
	sort.Slice(plan.Slots, func(i, j int) bool { return plan.Slots[i].Start < plan.Slots[j].Start })
	return plan, nil
}
