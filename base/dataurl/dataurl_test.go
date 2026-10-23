package dataurl

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"testing"
)

// knownFailures mirrors data-url/tests/wpt.rs:39: WPT expects this input to
// be rejected; the crate (and so this port) accepts it.
var knownFailures = map[string]bool{"data://test:test/,X": true}

// data-url/tests/wpt.rs:6-63 over testdata/data-urls.json (WPT).
func TestWPTDataURLs(t *testing.T) {
	var cases [][]json.RawMessage
	readJSON(t, "testdata/data-urls.json", &cases)
	ran := 0
	for _, c := range cases {
		var input string
		var mime *string
		var body []byte
		json.Unmarshal(c[0], &input)
		json.Unmarshal(c[1], &mime)
		hasBody := len(c) == 3
		if hasBody {
			var ints []int
			json.Unmarshal(c[2], &ints)
			for _, v := range ints {
				body = append(body, byte(v))
			}
		}
		ran++
		ok := runDataURL(input, mime, body, hasBody)
		if ok == knownFailures[input] {
			t.Errorf("%q: pass=%v, known failure=%v", input, ok, knownFailures[input])
		}
	}
	if ran < 60 {
		t.Fatalf("only %d cases", ran)
	}
}

func runDataURL(input string, mime *string, body []byte, hasBody bool) bool {
	u, err := Process(input)
	if mime != nil {
		if err != nil {
			return false
		}
		got, _, err := u.DecodeToVec()
		if err != nil {
			return false
		}
		want := *mime
		if want == "" {
			want = "text/plain;charset=US-ASCII"
		}
		if u.MimeType().String() != want {
			return false
		}
		return !hasBody || bytes.Equal(got, body)
	}
	if err == nil {
		_, _, derr := u.DecodeToVec()
		return derr != nil
	}
	return true
}

// data-url/tests/wpt.rs:65-91 over testdata/base64.json (WPT).
func TestWPTForgivingBase64(t *testing.T) {
	var cases [][]json.RawMessage
	readJSON(t, "testdata/base64.json", &cases)
	for _, c := range cases {
		var input string
		var want *[]int
		json.Unmarshal(c[0], &input)
		json.Unmarshal(c[1], &want)
		got, err := DecodeBase64([]byte(input))
		switch {
		case want == nil && err == nil:
			t.Errorf("%q: expected error, got %v", input, got)
		case want != nil && err != nil:
			t.Errorf("%q: unexpected error %v", input, err)
		case want != nil:
			var wb []byte
			for _, v := range *want {
				wb = append(wb, byte(v))
			}
			if !bytes.Equal(got, wb) && !(len(got) == 0 && len(wb) == 0) {
				t.Errorf("%q: got %v want %v", input, got, wb)
			}
		}
	}
}

// data-url/tests/wpt.rs:93-144 over the WPT MIME type files.
func TestWPTMimeTypes(t *testing.T) {
	ran := 0
	for _, file := range []string{"testdata/mime-types.json", "testdata/generated-mime-types.json"} {
		var entries []json.RawMessage
		readJSON(t, file, &entries)
		for _, e := range entries {
			var c struct {
				Input  *string `json:"input"`
				Output *string `json:"output"`
			}
			if json.Unmarshal(e, &c) != nil || c.Input == nil {
				continue // a comment string
			}
			ran++
			m, err := ParseMime(*c.Input)
			switch {
			case c.Output == nil && err == nil:
				t.Errorf("%q: expected error, got %q", *c.Input, m.String())
			case c.Output != nil && err != nil:
				t.Errorf("%q: unexpected error", *c.Input)
			case c.Output != nil && m.String() != *c.Output:
				t.Errorf("%q: got %q want %q", *c.Input, m.String(), *c.Output)
			}
		}
	}
	if ran < 500 {
		t.Fatalf("only %d MIME cases", ran)
	}
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// The data-url half of text/utils.rs:74-100 test_data_url_image: the MIME
// type and decoded bytes data_url_image feeds to ImageFormat::from_mime_type
// (that mapping, including image/jpg -> Jpeg, is gpui's).
func TestBaseDataURLImage(t *testing.T) {
	for _, c := range []struct {
		url, mime string
		want      []byte
	}{
		{"data:image/png;base64,iVBORw0KGgo=", "image/png", []byte("\x89PNG\r\n\x1a\n")},
		{"data:image/jpg;base64,/9j/4A==", "image/jpg", []byte("\xff\xd8\xff\xe0")},
		{"data:image/svg+xml,%3Csvg%3E%3C/svg%3E", "image/svg+xml", []byte("<svg></svg>")},
		{"data:text/plain;base64,aGVsbG8=", "text/plain", []byte("hello")}, // decodes; Base rejects it by MIME
	} {
		u, err := Process(c.url)
		if err != nil {
			t.Fatalf("%q: %v", c.url, err)
		}
		m := u.MimeType()
		got, _, err := u.DecodeToVec()
		if err != nil || m.Type+"/"+m.Subtype != c.mime || !bytes.Equal(got, c.want) {
			t.Errorf("%q: %s/%s %q %v", c.url, m.Type, m.Subtype, got, err)
		}
	}
	if _, err := Process("https://example.com/logo.png"); !errors.Is(err, ErrNotADataURL) {
		t.Errorf("https: %v", err)
	}
	if _, err := Process("data:image/png"); !errors.Is(err, ErrNoComma) {
		t.Errorf("no comma: %v", err)
	}
	u, _ := Process("data:image/png;base64,not*base64")
	if _, _, err := u.DecodeToVec(); !errors.Is(err, ErrInvalidBase64) {
		t.Errorf("bad base64: %v", err)
	}
}

func TestFragmentAndCrateDocExample(t *testing.T) {
	// data-url/src/lib.rs:5-15
	u, err := Process("data:,Hello%20World!")
	if err != nil {
		t.Fatal(err)
	}
	body, frag, err := u.DecodeToVec()
	if err != nil || string(body) != "Hello World!" || frag != nil {
		t.Fatalf("doc example: %q %v %v", body, frag, err)
	}
	if !u.MimeType().Matches("text", "plain") {
		t.Fatal("matches")
	}
	if cs, _ := u.MimeType().GetParameter("charset"); cs != "US-ASCII" {
		t.Fatal("charset")
	}
	u, _ = Process(" DATA:image/PNG ; base64 , iVBO Rw0K\tGgo#a b\"<")
	body, frag, err = u.DecodeToVec()
	if err != nil || string(body) != "\x89PNG\r\n\x1a\n" || frag == nil || frag.ToPercentEncoded() != "a%20b%22%3C" {
		t.Fatalf("fragment: %q %v %v", body, frag, err)
	}
	if u.MimeType().String() != "image/png" {
		t.Fatalf("mime %q", u.MimeType().String())
	}
	// A write error is returned unchanged.
	stop := errors.New("stop")
	if _, err := u.Decode(func([]byte) error { return stop }); err != stop {
		t.Fatalf("write error: %v", err)
	}
}
