package content

import (
	"errors"
	"strings"
	"testing"
)

func TestBuild_TextAndURL(t *testing.T) {
	for _, typ := range []string{"", "text", "url", "unknown"} {
		got, err := Build(typ, map[string]string{"data": "hello"})
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", typ, err)
		}
		if got != "hello" {
			t.Errorf("type %q: got %q, want %q", typ, got, "hello")
		}
	}
}

func TestBuild_WiFi(t *testing.T) {
	got, err := Build("wifi", map[string]string{
		"ssid": "My;Net", "password": "p@ss", "encryption": "WPA", "hidden": "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `WIFI:T:WPA;S:My\;Net;P:p@ss;H:true;;`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuild_WiFi_Nopass(t *testing.T) {
	got, err := Build("wifi", map[string]string{"ssid": "Open", "encryption": "nopass", "password": "ignored"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "T:nopass") || !strings.Contains(got, "P:;") {
		t.Errorf("nopass should clear password: %q", got)
	}
}

func TestBuild_MissingRequired(t *testing.T) {
	cases := map[string]map[string]string{
		"wifi":  {},
		"tel":   {},
		"geo":   {"lat": "1"},
		"email": {},
	}
	for typ, q := range cases {
		_, err := Build(typ, q)
		if !errors.Is(err, ErrInvalidContent) {
			t.Errorf("type %q: expected ErrInvalidContent, got %v", typ, err)
		}
	}
}

func TestBuild_Simple(t *testing.T) {
	cases := []struct {
		typ  string
		q    map[string]string
		want string
	}{
		{"tel", map[string]string{"number": "+628123"}, "tel:+628123"},
		{"sms", map[string]string{"number": "123", "message": "hi"}, "SMSTO:123:hi"},
		{"geo", map[string]string{"lat": "1.5", "lng": "2.5"}, "geo:1.5,2.5"},
		{"email", map[string]string{"to": "a@b.com"}, "mailto:a@b.com"},
		{"whatsapp", map[string]string{"number": "+62 812-3456"}, "https://wa.me/628123456"},
		{"whatsapp", map[string]string{"number": "0812-3456-7890"}, "https://wa.me/6281234567890"},
		{"whatsapp", map[string]string{"number": "(0812) 3456.7890"}, "https://wa.me/6281234567890"},
		{"whatsapp", map[string]string{"number": "0062 812 3456"}, "https://wa.me/628123456"},
		{"whatsapp", map[string]string{"number": "0412 345 678", "country": "+61"}, "https://wa.me/61412345678"},
		{"whatsapp", map[string]string{"number": "081234567", "message": "Halo, apa kabar?"}, "https://wa.me/6281234567?text=Halo%2C%20apa%20kabar%3F"},
	}
	for _, c := range cases {
		got, err := Build(c.typ, c.q)
		if err != nil {
			t.Fatalf("type %q: %v", c.typ, err)
		}
		if got != c.want {
			t.Errorf("type %q: got %q, want %q", c.typ, got, c.want)
		}
	}
}

func TestBuild_VCardAndEvent(t *testing.T) {
	v, err := Build("vcard", map[string]string{"name": "Eka", "email": "e@x.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(v, "BEGIN:VCARD") || !strings.Contains(v, "EMAIL:e@x.com") {
		t.Errorf("bad vcard: %q", v)
	}
	e, err := Build("event", map[string]string{"title": "Launch", "location": "HQ"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e, "SUMMARY:Launch") || !strings.Contains(e, "LOCATION:HQ") {
		t.Errorf("bad event: %q", e)
	}
}

func TestBuild_WhatsAppRejectsBadNumbers(t *testing.T) {
	for _, n := range []string{"abc", "0", "12345", "1234567890123456"} {
		if _, err := Build("whatsapp", map[string]string{"number": n}); err == nil {
			t.Errorf("number %q should be rejected", n)
		}
	}
}
