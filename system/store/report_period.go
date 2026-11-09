package store

import "time"

// PreviousWeekReport selects the last completed local Monday-Sunday week.
// This uses the store's configured timezone rather than the UTC scheduler
// clock, so reports do not straddle a local midnight or DST transition.
func(s *Store)PreviousWeekReport(at time.Time)(Report,error){
 if at.IsZero(){at=time.Now()}
 prior:=at.In(s.zone).AddDate(0,0,-7)
 return s.Report(prior.Format("2006-01-02"))
}
