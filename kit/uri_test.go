package kit

import "testing"

func TestParseURIRoundTrip(t *testing.T) {
	cases := []struct {
		in        string
		scheme    string
		authority string
		id        string
		frag      string
	}{
		{"goodreads://book/12345", "goodreads", "book", "12345", ""},
		{"x://status/1700000000000000000", "x", "status", "1700000000000000000", ""},
		{"goodreads://author/153394.Suzanne_Collins", "goodreads", "author", "153394.Suzanne_Collins", ""},
		{"archive://web/20230101000000/https%3A%2F%2Fexample.com", "archive", "web", "20230101000000/https://example.com", ""},
		{"goodreads://book/12345#reviews", "goodreads", "book", "12345", "reviews"},
	}
	for _, c := range cases {
		u, err := ParseURI(c.in)
		if err != nil {
			t.Fatalf("ParseURI(%q): %v", c.in, err)
		}
		if u.Scheme != c.scheme || u.Authority != c.authority {
			t.Errorf("ParseURI(%q) scheme/authority = %q/%q, want %q/%q", c.in, u.Scheme, u.Authority, c.scheme, c.authority)
		}
		if u.ID() != c.id {
			t.Errorf("ParseURI(%q).ID() = %q, want %q", c.in, u.ID(), c.id)
		}
		if u.Fragment != c.frag {
			t.Errorf("ParseURI(%q).Fragment = %q, want %q", c.in, u.Fragment, c.frag)
		}
	}
}

func TestURIStringCanonicalizesQuery(t *testing.T) {
	u := URI{
		Scheme:    "search",
		Authority: "goodreads.com",
		Path:      []string{"hunger games"},
		Query:     map[string]string{"page": "2", "lang": "en"},
	}
	// Query keys sort, so equal URIs serialize identically regardless of map order.
	got := u.String()
	want := "search://goodreads.com/hunger%20games?lang=en&page=2"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestParseURIRejectsNonURI(t *testing.T) {
	for _, in := range []string{"12345", "@handle", "not a uri", "https://goodreads.com/book/show/1", "://x"} {
		if _, err := ParseURI(in); err == nil {
			t.Errorf("ParseURI(%q) = nil error, want error", in)
		}
	}
}

func TestDataPathEncodesSlashes(t *testing.T) {
	u, err := ParseURI("archive://web/20230101000000/https%3A%2F%2Fexample.com")
	if err != nil {
		t.Fatal(err)
	}
	got := u.DataPath()
	want := "archive/web/20230101000000/https:%2F%2Fexample.com"
	if got != want {
		t.Errorf("DataPath() = %q, want %q", got, want)
	}
}

func TestReservedKinds(t *testing.T) {
	for _, k := range []string{"host", "pages", "feed", "search", "data"} {
		if !IsReservedKind(k) {
			t.Errorf("IsReservedKind(%q) = false, want true", k)
		}
	}
	if IsReservedKind("goodreads") {
		t.Error("IsReservedKind(goodreads) = true, want false")
	}
}
