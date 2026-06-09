package controller

import (
	"strings"
	"testing"
)

func TestIsValidCustomMenuURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"http", "http://example.com", true},
		{"https", "https://example.com/path?x=1", true},
		{"relative", "/about", true},
		{"relative deep", "/dashboard/overview", true},
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"protocol relative", "//evil.com", false},
		{"javascript", "javascript:alert(1)", false},
		{"javascript uppercase", "JavaScript:alert(1)", false},
		{"data uri", "data:text/html,<script>", false},
		{"vbscript", "vbscript:msgbox(1)", false},
		{"ftp", "ftp://example.com", false},
		{"no scheme bare host", "example.com", false},
		{"http with no host", "http://", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := isValidCustomMenuURL(c.url)
			if got != c.want {
				t.Errorf("isValidCustomMenuURL(%q) = %v, want %v", c.url, got, c.want)
			}
		})
	}
}

func TestValidateCustomMenuPages_Empty(t *testing.T) {
	if err := validateCustomMenuPages(""); err != nil {
		t.Errorf("empty raw should pass, got: %v", err)
	}
	if err := validateCustomMenuPages("   "); err != nil {
		t.Errorf("whitespace raw should pass, got: %v", err)
	}
}

func TestValidateCustomMenuPages_OK(t *testing.T) {
	raw := `{"items":[
		{"id":"a","name":"首页","url":"/home","visibleTo":"user","openMode":"iframe","enabled":true},
		{"id":"b","name":"Docs","url":"https://docs.example.com","visibleTo":"admin","openMode":"newWindow","enabled":false}
	]}`
	if err := validateCustomMenuPages(raw); err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}

func TestValidateCustomMenuPages_OpenModeMissingIsOK(t *testing.T) {
	// Missing openMode should be accepted (legacy data → defaults to iframe on read).
	raw := `{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"user","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err != nil {
		t.Errorf("missing openMode should pass, got: %v", err)
	}
}

func TestValidateCustomMenuPages_OpenModeInvalid(t *testing.T) {
	raw := `{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"user","openMode":"popup","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected invalid openMode error")
	}
}

func TestValidateCustomMenuPages_LayoutModeMissingIsOK(t *testing.T) {
	// Missing layoutMode should be accepted (legacy data → defaults to sidebar on read).
	raw := `{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"user","openMode":"iframe","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err != nil {
		t.Errorf("missing layoutMode should pass, got: %v", err)
	}
}

func TestValidateCustomMenuPages_LayoutModeValid(t *testing.T) {
	cases := []string{
		`{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"user","openMode":"iframe","layoutMode":"sidebar","enabled":false}]}`,
		`{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"user","openMode":"iframe","layoutMode":"fullwidth","enabled":false}]}`,
	}
	for i, raw := range cases {
		if err := validateCustomMenuPages(raw); err != nil {
			t.Errorf("case %d: expected valid layoutMode, got: %v", i, err)
		}
	}
}

func TestValidateCustomMenuPages_LayoutModeInvalid(t *testing.T) {
	raw := `{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"user","openMode":"iframe","layoutMode":"floating","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected invalid layoutMode error")
	}
}

func TestValidateCustomMenuPages_BadJSON(t *testing.T) {
	if err := validateCustomMenuPages(`{"items":[`); err == nil {
		t.Errorf("expected parse error")
	}
}

func buildItems(n int, enabled int) string {
	var b strings.Builder
	b.WriteString(`{"items":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		en := "false"
		if i < enabled {
			en = "true"
		}
		b.WriteString(`{"id":"id`)
		b.WriteString(intStr(i))
		b.WriteString(`","name":"n`)
		b.WriteString(intStr(i))
		b.WriteString(`","url":"/a","visibleTo":"user","enabled":`)
		b.WriteString(en)
		b.WriteString(`}`)
	}
	b.WriteString(`]}`)
	return b.String()
}

func intStr(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

func TestValidateCustomMenuPages_TooManyItems(t *testing.T) {
	raw := buildItems(21, 0)
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected too-many-items error")
	}
}

func TestValidateCustomMenuPages_TooManyEnabled(t *testing.T) {
	raw := buildItems(15, 11)
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected too-many-enabled error")
	}
}

func TestValidateCustomMenuPages_NameTooLong(t *testing.T) {
	raw := `{"items":[{"id":"a","name":"abcdef","url":"/x","visibleTo":"user","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected name-too-long error")
	}
}

func TestValidateCustomMenuPages_NameEmpty(t *testing.T) {
	raw := `{"items":[{"id":"a","name":"  ","url":"/x","visibleTo":"user","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected name-empty error")
	}
}

func TestValidateCustomMenuPages_BadURL(t *testing.T) {
	cases := []string{
		`{"items":[{"id":"a","name":"x","url":"javascript:alert(1)","visibleTo":"user","enabled":false}]}`,
		`{"items":[{"id":"a","name":"x","url":"//evil.com","visibleTo":"user","enabled":false}]}`,
		`{"items":[{"id":"a","name":"x","url":"","visibleTo":"user","enabled":false}]}`,
		`{"items":[{"id":"a","name":"x","url":"ftp://x","visibleTo":"user","enabled":false}]}`,
	}
	for i, raw := range cases {
		if err := validateCustomMenuPages(raw); err == nil {
			t.Errorf("case %d: expected bad-url error", i)
		}
	}
}

func TestValidateCustomMenuPages_BadVisibleTo(t *testing.T) {
	raw := `{"items":[{"id":"a","name":"x","url":"/x","visibleTo":"guest","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected bad-visibleTo error")
	}
}

func TestValidateCustomMenuPages_DuplicateID(t *testing.T) {
	raw := `{"items":[
		{"id":"a","name":"x","url":"/x","visibleTo":"user","enabled":false},
		{"id":"a","name":"y","url":"/y","visibleTo":"user","enabled":false}
	]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected duplicate-id error")
	}
}

func TestValidateCustomMenuPages_MissingID(t *testing.T) {
	raw := `{"items":[{"id":"   ","name":"x","url":"/x","visibleTo":"user","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("expected missing-id error")
	}
}

func TestValidateCustomMenuPages_CJKName(t *testing.T) {
	raw := `{"items":[{"id":"a","name":"中文五字测","url":"/x","visibleTo":"user","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err != nil {
		t.Errorf("CJK 5 chars should pass, got: %v", err)
	}
	raw = `{"items":[{"id":"a","name":"中文超过五字限","url":"/x","visibleTo":"user","enabled":false}]}`
	if err := validateCustomMenuPages(raw); err == nil {
		t.Errorf("CJK 7 chars should fail")
	}
}

func TestValidateCustomMenuPages_BoundaryCounts(t *testing.T) {
	if err := validateCustomMenuPages(buildItems(20, 10)); err != nil {
		t.Errorf("boundary 20/10 should pass, got: %v", err)
	}
	if err := validateCustomMenuPages(buildItems(20, 11)); err == nil {
		t.Errorf("20/11 enabled should fail")
	}
}
