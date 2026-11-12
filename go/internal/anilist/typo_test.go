package anilist

import (
	"reflect"
	"testing"
)

func TestTypoFallbacks(t *testing.T) {
	got := typoFallbacks("I will fall with the emporer")
	want := []string{"I will fall with the emp", "I will fall with the"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if typoFallbacks("solo") != nil {
		t.Fatal("single words have no fallback")
	}
}
