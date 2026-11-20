package httpapi

import (
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/herezi/herezi-api/internal/domain"
)

// 계약 테스트: api/openapi.yaml(단일 기준)과 코드가 어긋나면 실패한다.

type openAPI struct {
	Paths map[string]map[string]struct {
		Parameters []struct {
			Name   string `yaml:"name"`
			Schema struct {
				Enum    []string `yaml:"enum"`
				Default any      `yaml:"default"`
				Minimum *float64 `yaml:"minimum"`
				Maximum *float64 `yaml:"maximum"`
			} `yaml:"schema"`
		} `yaml:"parameters"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Enum       []string       `yaml:"enum"`
			Properties map[string]any `yaml:"properties"`
		} `yaml:"schemas"`
	} `yaml:"components"`
}

func loadOpenAPI(t *testing.T) openAPI {
	t.Helper()
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc openAPI
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestCategoriesMatchOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	var got []string
	for _, c := range domain.Categories() {
		got = append(got, string(c))
	}
	want := doc.Components.Schemas["Category"].Enum
	if !slices.Equal(got, want) {
		t.Fatalf("domain.Categories() = %v, openapi Category enum = %v", got, want)
	}
}

func TestWhenMatchesOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	var want []string
	for _, p := range doc.Paths["/v1/spots"]["get"].Parameters {
		if p.Name == "when" {
			want = p.Schema.Enum
		}
	}
	if len(want) == 0 {
		t.Fatal("when enum not found in openapi")
	}
	for _, w := range want {
		if _, err := domain.ParseWhen(w); err != nil {
			t.Errorf("openapi when %q not accepted: %v", w, err)
		}
	}
	for _, w := range []domain.When{domain.WhenNow, domain.WhenToday, domain.WhenWeekend, domain.WhenWeek} {
		if !slices.Contains(want, string(w)) {
			t.Errorf("domain when %q missing from openapi", w)
		}
	}
}

func TestRoutesMatchOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	var want []string
	for path, ops := range doc.Paths {
		for method := range ops {
			if method == "parameters" {
				continue
			}
			want = append(want, strings.ToUpper(method)+" "+path)
		}
	}
	var got []string
	for _, r := range (&Server{}).Routes() {
		got = append(got, r.Method+" "+r.Path)
	}
	sort.Strings(want)
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Fatalf("routes = %v, openapi = %v", got, want)
	}
}

func TestJSONFieldsMatchOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	for name, typ := range map[string]reflect.Type{
		"Spot":     reflect.TypeFor[spotJSON](),
		"SpotPage": reflect.TypeFor[spotPageJSON](),
		"SpotMap":  reflect.TypeFor[spotMapJSON](),
		"Error":    reflect.TypeFor[errorBody](),
	} {
		var got, want []string
		for i := range typ.NumField() {
			got = append(got, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
		}
		for p := range doc.Components.Schemas[name].Properties {
			want = append(want, p)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: json fields %v, openapi properties %v", name, got, want)
		}
	}
}

// TestQueryParamsMatchOpenAPI checks parameter names and the integer limits the handlers enforce.
func TestQueryParamsMatchOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	type lim struct{ def, lo, hi int }
	for path, want := range map[string]struct {
		names  []string
		limits map[string]lim
	}{
		"/v1/spots": {
			names: []string{"category", "cursor", "free", "lat", "limit", "lng", "radius_m", "when"},
			limits: map[string]lim{
				"radius_m": {defaultRadiusM, minRadiusM, maxRadiusM},
				"limit":    {defaultLimit, 1, maxLimit},
			},
		},
		"/v1/spots/map": {
			names:  []string{"category", "free", "limit", "ne_lat", "ne_lng", "sw_lat", "sw_lng", "when"},
			limits: map[string]lim{"limit": {defaultMapLimit, 1, maxMapLimit}},
		},
	} {
		var names []string
		for _, p := range doc.Paths[path]["get"].Parameters {
			names = append(names, p.Name)
			l, ok := want.limits[p.Name]
			if !ok {
				continue
			}
			sc := p.Schema
			if sc.Minimum == nil || sc.Maximum == nil || int(*sc.Minimum) != l.lo || int(*sc.Maximum) != l.hi || sc.Default != l.def {
				t.Errorf("%s %s: openapi default=%v min=%v max=%v, code %+v", path, p.Name, sc.Default, sc.Minimum, sc.Maximum, l)
			}
		}
		sort.Strings(names)
		if !slices.Equal(names, want.names) {
			t.Errorf("%s: openapi params %v, handled %v", path, names, want.names)
		}
	}
}
