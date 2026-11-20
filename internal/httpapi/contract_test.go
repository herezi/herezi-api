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
				Enum []string `yaml:"enum"`
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
