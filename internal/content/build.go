// Package content builds standard QR payload strings from a content type and a
// set of fields. It lets the API accept structured input (WiFi, vCard, email,
// etc.) instead of requiring callers to hand-craft the encoded string.
//
// It is intentionally dependency-free and stateless: Build takes the content
// type and a flat map of fields (e.g. Fiber's c.Queries()) and returns the
// string to encode in the QR code.
package content

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInvalidContent is returned when required fields for a content type are
// missing or malformed. Callers should map this to an HTTP 400.
var ErrInvalidContent = errors.New("invalid content parameters for the requested type")

// Build converts a content type and its fields into the raw string to encode.
//
// For "text", "url", or an empty type, the "data" field is returned verbatim.
// All other types read their own fields (ssid, password, number, ...) and
// ignore "data". Unknown types fall back to "data" so callers never break.
func Build(typ string, q map[string]string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "", "text", "url":
		return q["data"], nil
	case "wifi":
		return buildWiFi(q)
	case "vcard":
		return buildVCard(q)
	case "mecard":
		return buildMeCard(q)
	case "email", "mailto":
		return buildEmail(q)
	case "tel", "phone":
		return buildTel(q)
	case "sms":
		return buildSMS(q)
	case "geo":
		return buildGeo(q)
	case "event":
		return buildEvent(q)
	case "whatsapp", "wa":
		return buildWhatsApp(q)
	default:
		// Unknown type: degrade gracefully to raw data rather than erroring.
		return q["data"], nil
	}
}

func require(q map[string]string, field string) (string, error) {
	v := strings.TrimSpace(q[field])
	if v == "" {
		return "", fmt.Errorf("%w: missing %q", ErrInvalidContent, field)
	}
	return v, nil
}

// escapeWiFi escapes the special characters \ ; , : " used in WIFI/MECARD payloads.
func escapeWiFi(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`;`, `\;`,
		`,`, `\,`,
		`:`, `\:`,
		`"`, `\"`,
	)
	return r.Replace(s)
}

func buildWiFi(q map[string]string) (string, error) {
	ssid, err := require(q, "ssid")
	if err != nil {
		return "", err
	}
	enc := strings.ToUpper(strings.TrimSpace(q["encryption"]))
	switch enc {
	case "", "WPA", "WPA2":
		enc = "WPA"
	case "WEP":
		enc = "WEP"
	case "NOPASS", "NONE":
		enc = "nopass"
	default:
		return "", fmt.Errorf("%w: encryption must be WPA, WEP or nopass", ErrInvalidContent)
	}
	pass := q["password"]
	if enc == "nopass" {
		pass = ""
	}
	hidden := "false"
	if b := strings.ToLower(strings.TrimSpace(q["hidden"])); b == "true" || b == "1" || b == "yes" {
		hidden = "true"
	}
	return fmt.Sprintf("WIFI:T:%s;S:%s;P:%s;H:%s;;",
		enc, escapeWiFi(ssid), escapeWiFi(pass), hidden), nil
}

func buildMeCard(q map[string]string) (string, error) {
	name, err := require(q, "name")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("MECARD:N:")
	sb.WriteString(escapeWiFi(name))
	sb.WriteString(";")
	for _, f := range []struct{ tag, key string }{
		{"TEL", "phone"}, {"EMAIL", "email"}, {"URL", "url"},
		{"ADR", "address"}, {"ORG", "org"},
	} {
		if v := strings.TrimSpace(q[f.key]); v != "" {
			sb.WriteString(f.tag)
			sb.WriteString(":")
			sb.WriteString(escapeWiFi(v))
			sb.WriteString(";")
		}
	}
	sb.WriteString(";")
	return sb.String(), nil
}

func buildVCard(q map[string]string) (string, error) {
	name, err := require(q, "name")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("BEGIN:VCARD\nVERSION:3.0\n")
	fmt.Fprintf(&sb, "N:%s\nFN:%s\n", name, name)
	if v := strings.TrimSpace(q["org"]); v != "" {
		fmt.Fprintf(&sb, "ORG:%s\n", v)
	}
	if v := strings.TrimSpace(q["phone"]); v != "" {
		fmt.Fprintf(&sb, "TEL:%s\n", v)
	}
	if v := strings.TrimSpace(q["email"]); v != "" {
		fmt.Fprintf(&sb, "EMAIL:%s\n", v)
	}
	if v := strings.TrimSpace(q["url"]); v != "" {
		fmt.Fprintf(&sb, "URL:%s\n", v)
	}
	if v := strings.TrimSpace(q["address"]); v != "" {
		fmt.Fprintf(&sb, "ADR:%s\n", v)
	}
	sb.WriteString("END:VCARD")
	return sb.String(), nil
}

func buildEmail(q map[string]string) (string, error) {
	to, err := require(q, "to")
	if err != nil {
		return "", err
	}
	params := url.Values{}
	if v := strings.TrimSpace(q["subject"]); v != "" {
		params.Set("subject", v)
	}
	if v := strings.TrimSpace(q["body"]); v != "" {
		params.Set("body", v)
	}
	out := "mailto:" + to
	if len(params) > 0 {
		out += "?" + params.Encode()
	}
	return out, nil
}

func buildTel(q map[string]string) (string, error) {
	num, err := require(q, "number")
	if err != nil {
		return "", err
	}
	return "tel:" + num, nil
}

func buildSMS(q map[string]string) (string, error) {
	num, err := require(q, "number")
	if err != nil {
		return "", err
	}
	return "SMSTO:" + num + ":" + q["message"], nil
}

func buildGeo(q map[string]string) (string, error) {
	lat, err := require(q, "lat")
	if err != nil {
		return "", err
	}
	lng, err := require(q, "lng")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("geo:%s,%s", lat, lng), nil
}

func buildEvent(q map[string]string) (string, error) {
	title, err := require(q, "title")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("BEGIN:VEVENT\n")
	fmt.Fprintf(&sb, "SUMMARY:%s\n", title)
	if v := strings.TrimSpace(q["start"]); v != "" {
		fmt.Fprintf(&sb, "DTSTART:%s\n", v)
	}
	if v := strings.TrimSpace(q["end"]); v != "" {
		fmt.Fprintf(&sb, "DTEND:%s\n", v)
	}
	if v := strings.TrimSpace(q["location"]); v != "" {
		fmt.Fprintf(&sb, "LOCATION:%s\n", v)
	}
	sb.WriteString("END:VEVENT")
	return sb.String(), nil
}

func buildWhatsApp(q map[string]string) (string, error) {
	num, err := require(q, "number")
	if err != nil {
		return "", err
	}
	// wa.me expects digits only (no +, spaces or dashes).
	num = strings.NewReplacer("+", "", " ", "", "-", "", "(", "", ")", "").Replace(num)
	out := "https://wa.me/" + num
	if msg := strings.TrimSpace(q["message"]); msg != "" {
		out += "?text=" + url.QueryEscape(msg)
	}
	return out, nil
}
