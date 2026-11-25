package domain

import (
	"math"
	"testing"
	"time"
)

func TestParseCategories(t *testing.T) {
	got, err := ParseCategories("festival, popup")
	if err != nil || len(got) != 2 || got[0] != CategoryFestival || got[1] != CategoryPopup {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := ParseCategories("festival,bogus"); err == nil {
		t.Fatal("want error for unknown category")
	}
	if got, err := ParseCategories(""); err != nil || got != nil {
		t.Fatalf("empty: got %v, %v", got, err)
	}
}

func TestWhenWindow(t *testing.T) {
	// Wednesday 2026-11-18 15:00 KST
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, Seoul)
	from, to := WhenWeekend.Window(now)
	if from.Weekday() != time.Saturday || from.Hour() != 0 {
		t.Errorf("weekend from = %v", from)
	}
	if to.In(Seoul).Weekday() != time.Sunday || to.In(Seoul).Hour() != 23 {
		t.Errorf("weekend to = %v", to)
	}
	from, to = WhenToday.Window(now)
	if !from.Equal(now) || to.In(Seoul).Day() != 18 {
		t.Errorf("today = %v..%v", from, to)
	}
	// Sunday: weekend starts now, ends tonight.
	sun := time.Date(2026, 11, 22, 10, 0, 0, 0, Seoul)
	from, to = WhenWeekend.Window(sun)
	if !from.Equal(sun) || to.In(Seoul).Day() != 22 {
		t.Errorf("weekend on sunday = %v..%v", from, to)
	}
}

func TestOverlaps(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, Seoul)
	end := now.Add(-time.Hour)
	finished := Spot{StartsAt: now.Add(-2 * time.Hour), EndsAt: &end}
	if finished.Overlaps(WhenNow.Window(now)) {
		t.Error("finished spot must not overlap now")
	}
	later := now.Add(time.Hour)
	ongoing := Spot{StartsAt: now.Add(-time.Hour), EndsAt: &later}
	if !ongoing.Overlaps(WhenNow.Window(now)) {
		t.Error("ongoing spot must overlap now")
	}
}

func TestDistanceM(t *testing.T) {
	// Seoul City Hall -> Gwanghwamun, roughly 1.07km.
	d := DistanceM(37.5663, 126.9779, 37.5759, 126.9768)
	if math.Abs(d-1070) > 200 {
		t.Errorf("distance = %v", d)
	}
}

func TestEndedAndFree(t *testing.T) {
	now := time.Date(2026, 11, 18, 15, 0, 0, 0, Seoul)
	past, future := now.Add(-time.Minute), now.Add(time.Minute)
	if !(Spot{EndsAt: &past}).Ended(now) || (Spot{EndsAt: &future}).Ended(now) || (Spot{}).Ended(now) {
		t.Error("Ended wrong")
	}
	yes, no := true, false
	if !(Spot{IsFree: &yes}).Free() || (Spot{IsFree: &no}).Free() || (Spot{}).Free() {
		t.Error("Free wrong")
	}
}

func TestCategoriesHaveNoFree(t *testing.T) {
	if Category("free").Valid() {
		t.Error("free is a filter, not a category (SERVICE-PLAN.md §12 Q3)")
	}
	if len(Categories()) != 9 {
		t.Errorf("want 9 categories, got %d", len(Categories()))
	}
}

func TestMatchesKeyword(t *testing.T) {
	sp := Spot{Title: "Seoul Lantern 빛초롱", PlaceName: "청계천", Address: "서울 중구 50%_로"}
	for kw, want := range map[string]bool{
		"":         true,
		"lantern":  true,
		"LANTERN":  true,
		"빛초롱":      true,
		"청계":       true,
		"중구":       true,
		"%_":       true,
		"성수":       false,
		"lanterns": false,
	} {
		if got := sp.MatchesKeyword(kw); got != want {
			t.Errorf("MatchesKeyword(%q) = %v, want %v", kw, got, want)
		}
	}
}

func TestRegions(t *testing.T) {
	r, ok := RegionByID("seoul")
	if !ok || !r.BBox.Contains(37.5663, 126.9779) || r.BBox.Contains(35.1796, 129.0756) {
		t.Fatalf("seoul = %+v, %v", r, ok)
	}
	if err := r.BBox.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, ok := RegionByID("busan"); ok {
		t.Fatal("busan must be unknown")
	}
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, Seoul)
	from, to := RegionWindow(now)
	if !from.Equal(now.AddDate(0, 0, -RetainEndedDays)) || !to.Equal(now.AddDate(0, 0, RegionAheadDays)) {
		t.Fatalf("window %v ~ %v", from, to)
	}
}
