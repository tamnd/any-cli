package kit

import (
	"reflect"
	"testing"
)

// A driver with a dozen record kinds gives them one shared envelope and embeds
// it, so the id, the body and the links live on the embedded type. reflect's own
// field iterator stops at the embedded struct, which used to mean every one of
// those records looked like a record with no id at all.

type envelope struct {
	ID   string `json:"id" kit:"id"`
	Kind string `json:"kind"`
	Home string `json:"home,omitempty" kit:"link,kind=demo/user,optional"`
}

type post struct {
	envelope

	Text   string `json:"text" kit:"body"`
	Author string `json:"author" kit:"link,kind=demo/user"`
}

// shadowed declares its own id, which has to win over the embedded one the same
// way it does when you write the selector by hand.
type shadowed struct {
	envelope

	ID string `json:"slug" kit:"id"`
}

func TestIDFieldIsFoundThroughAnEmbeddedEnvelope(t *testing.T) {
	idx := idFieldIndex(reflect.TypeOf(post{}))
	if len(idx) != 2 {
		t.Fatalf("idFieldIndex = %v, want a path through the embedded envelope", idx)
	}
	p := post{}
	p.ID = "42"
	if got := reflect.ValueOf(p).FieldByIndex(idx).String(); got != "42" {
		t.Errorf("the id path reads %q, want 42", got)
	}
}

func TestBodyFieldStillWinsOnTheRecordItself(t *testing.T) {
	idx := bodyFieldIndex(reflect.TypeOf(post{}))
	if len(idx) != 1 {
		t.Fatalf("bodyFieldIndex = %v, want the record's own field", idx)
	}
}

func TestDeclaredFieldShadowsThePromotedOne(t *testing.T) {
	idx := idFieldIndex(reflect.TypeOf(shadowed{}))
	if len(idx) != 1 {
		t.Fatalf("idFieldIndex = %v, want the declared field, not the promoted one", idx)
	}
}

func TestLinksComeFromBothTheRecordAndItsEnvelope(t *testing.T) {
	got := map[string]bool{}
	for _, lf := range linkFields(reflect.TypeOf(post{})) {
		got[lf.jsonName] = true
	}
	for _, want := range []string{"author", "home"} {
		if !got[want] {
			t.Errorf("linkFields missing %q (got %v)", want, got)
		}
	}
}

func TestSchemaFlattensTheEnvelope(t *testing.T) {
	s, _ := schemaForType(reflect.TypeOf(post{}))["properties"].(map[string]any)
	for _, want := range []string{"id", "kind", "text", "author"} {
		if _, ok := s[want]; !ok {
			t.Errorf("schema missing %q (got %v)", want, s)
		}
	}
	if _, ok := s["envelope"]; ok {
		t.Error("the embedded struct came out as a property of its own")
	}
}
