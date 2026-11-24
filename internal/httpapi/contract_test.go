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
				Enum      []string `yaml:"enum"`
				Default   any      `yaml:"default"`
				Minimum   *float64 `yaml:"minimum"`
				Maximum   *float64 `yaml:"maximum"`
				MinLength *int     `yaml:"minLength"`
				MaxLength *int     `yaml:"maxLength"`
			} `yaml:"schema"`
		} `yaml:"parameters"`
	} `yaml:"paths"`
	Components struct {
		Schemas map[string]struct {
			Enum       []string       `yaml:"enum"`
			Required   []string       `yaml:"required"`
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
		"Spot":       reflect.TypeFor[spotJSON](),
		"SpotPage":   reflect.TypeFor[spotPageJSON](),
		"SpotMap":    reflect.TypeFor[spotMapJSON](),
		"Error":      reflect.TypeFor[errorBody](),
		"GeoReverse": reflect.TypeFor[geoReverseJSON](),
		// /v1/me/saved:import
		"SavedImportRequest": reflect.TypeFor[importSavedJSON](),
		"SavedImportResult":  reflect.TypeFor[importResultJSON](),
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

// TestRequiredFieldsMatchOpenAPI: openapi required 필드는 JSON에서 생략되면 안 되고(omitempty 금지),
// omitempty가 아닌 필드는 required여야 한다.
func TestRequiredFieldsMatchOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	for name, typ := range map[string]reflect.Type{
		"Spot":       reflect.TypeFor[spotJSON](),
		"SpotPage":   reflect.TypeFor[spotPageJSON](),
		"SpotMap":    reflect.TypeFor[spotMapJSON](),
		"Error":      reflect.TypeFor[errorBody](),
		"GeoReverse": reflect.TypeFor[geoReverseJSON](),
		// /v1/me/saved:import
		"SavedImportRequest": reflect.TypeFor[importSavedJSON](),
		"SavedImportResult":  reflect.TypeFor[importResultJSON](),
	} {
		var got []string
		for i := range typ.NumField() {
			tag := typ.Field(i).Tag.Get("json")
			if !strings.Contains(tag, ",omitempty") {
				got = append(got, strings.Split(tag, ",")[0])
			}
		}
		want := slices.Clone(doc.Components.Schemas[name].Required)
		sort.Strings(got)
		sort.Strings(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: always-present json fields %v, openapi required %v", name, got, want)
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
			names: []string{"category", "cursor", "free", "lat", "limit", "lng", "q", "radius_m", "when"},
			limits: map[string]lim{
				"radius_m": {defaultRadiusM, minRadiusM, maxRadiusM},
				"limit":    {defaultLimit, 1, maxLimit},
			},
		},
		"/v1/spots/map": {
			names:  []string{"category", "free", "limit", "ne_lat", "ne_lng", "sw_lat", "sw_lng", "when"},
			limits: map[string]lim{"limit": {defaultMapLimit, 1, maxMapLimit}},
		},
		"/v1/geo/reverse": {names: []string{"lat", "lng"}},
		"/v1/me/saved": {
			names:  []string{"cursor", "limit"},
			limits: map[string]lim{"limit": {defaultSavedLimit, 1, maxSavedLimit}},
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

// TestKeywordLengthMatchesOpenAPI: q의 minLength/maxLength가 핸들러 제한과 같아야 한다.
func TestKeywordLengthMatchesOpenAPI(t *testing.T) {
	doc := loadOpenAPI(t)
	for _, p := range doc.Paths["/v1/spots"]["get"].Parameters {
		if p.Name != "q" {
			continue
		}
		sc := p.Schema
		if sc.MinLength == nil || sc.MaxLength == nil || *sc.MinLength != minQueryRunes || *sc.MaxLength != maxQueryRunes {
			t.Fatalf("openapi q minLength=%v maxLength=%v, code [%d, %d]", sc.MinLength, sc.MaxLength, minQueryRunes, maxQueryRunes)
		}
		return
	}
	t.Fatal("q not found in openapi /v1/spots")
}

// TestMeContract: /v1/me/* 는 bearerAuth를 요구하고, 가져오기 개수·spot_id 길이 한도가 코드와 같아야 한다.
func TestMeContract(t *testing.T) {
	b, err := os.ReadFile("../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			Security   []map[string][]string `yaml:"security"`
			Parameters []struct {
				Name   string `yaml:"name"`
				Schema struct {
					MaxLength *int `yaml:"maxLength"`
				} `yaml:"schema"`
			} `yaml:"parameters"`
		} `yaml:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type   string `yaml:"type"`
				Scheme string `yaml:"scheme"`
			} `yaml:"securitySchemes"`
			Schemas map[string]struct {
				Properties map[string]struct {
					MinItems *int `yaml:"minItems"`
					MaxItems *int `yaml:"maxItems"`
					Items    struct {
						MaxLength *int `yaml:"maxLength"`
					} `yaml:"items"`
				} `yaml:"properties"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if sc := doc.Components.SecuritySchemes["bearerAuth"]; sc.Type != "http" || sc.Scheme != "bearer" {
		t.Fatalf("bearerAuth = %+v", sc)
	}
	for path, ops := range doc.Paths {
		for method, op := range ops {
			secured := len(op.Security) == 1 && op.Security[0]["bearerAuth"] != nil
			if strings.HasPrefix(path, "/v1/me") != secured {
				t.Errorf("%s %s: security %v (only /v1/me/* requires bearerAuth)", method, path, op.Security)
			}
			for _, p := range op.Parameters {
				if p.Name == "spot_id" && (p.Schema.MaxLength == nil || *p.Schema.MaxLength != maxSpotIDLen) {
					t.Errorf("%s %s spot_id maxLength = %v, code %d", method, path, p.Schema.MaxLength, maxSpotIDLen)
				}
			}
		}
	}
	ids := doc.Components.Schemas["SavedImportRequest"].Properties["spot_ids"]
	if ids.MinItems == nil || *ids.MinItems != 1 || ids.MaxItems == nil || *ids.MaxItems != maxImportIDs ||
		ids.Items.MaxLength == nil || *ids.Items.MaxLength != maxSpotIDLen {
		t.Fatalf("SavedImportRequest.spot_ids = %+v, code max %d / %d", ids, maxImportIDs, maxSpotIDLen)
	}
}
