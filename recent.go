package main

import (
	"sort"
	"time"
)

// RecentRows is presentation-only. It never contributes to cumulative counters.
type ViewRow struct {
	Name, Target, Down, Up string
	Rate                   float64
}
type RecentRow struct {
	ViewRow
	Kind   int
	Seen   time.Time
	Active bool
}
type RecentRows struct{ entries map[[3]string]RecentRow }

func (r *RecentRows) Update(input [2][]ViewRow, now time.Time, limit int) []RecentRow {
	if r.entries == nil {
		r.entries = make(map[[3]string]RecentRow)
	}
	for k, v := range r.entries {
		v.Active = false
		v.Rate = 0
		v.Down = "0 B/s"
		v.Up = "0 B/s"
		r.entries[k] = v
	}
	for kind, rows := range input {
		for _, row := range rows {
			if row.Rate <= 0 {
				continue
			}
			key := [3]string{routeName(kind), row.Name, row.Target}
			r.entries[key] = RecentRow{row, kind, now, true}
		}
	}
	type entry struct {
		key [3]string
		row RecentRow
	}
	a := make([]entry, 0, len(r.entries))
	for k, v := range r.entries {
		a = append(a, entry{k, v})
	}
	sort.Slice(a, func(i, j int) bool {
		x, y := a[i].row, a[j].row
		if x.Active != y.Active {
			return x.Active
		}
		if x.Active && x.Rate != y.Rate {
			return x.Rate > y.Rate
		}
		if !x.Seen.Equal(y.Seen) {
			return x.Seen.After(y.Seen)
		}
		if x.Kind != y.Kind {
			return x.Kind < y.Kind
		}
		if x.Name != y.Name {
			return x.Name < y.Name
		}
		return x.Target < y.Target
	})
	for i := maxRecentRows; i < len(a); i++ {
		delete(r.entries, a[i].key)
	}
	if limit < 0 {
		limit = 0
	}
	if limit > maxRecentRows {
		limit = maxRecentRows
	}
	if limit > len(a) {
		limit = len(a)
	}
	out := make([]RecentRow, 0, limit)
	for _, e := range a[:limit] {
		out = append(out, e.row)
	}
	return out
}
