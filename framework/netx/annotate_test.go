package netx

import (
	"errors"
	"strings"
	"testing"
)

func TestAnnotateError_RedactsPassword(t *testing.T) {
	const pw = "s3cret-pass"
	err := AnnotateError(Spec{ID: "office", Password: pw}, errors.New(`proxyconnect: http://alice:`+pw+`@127.0.0.1:8080`))
	if err == nil {
		t.Fatal("want annotated error")
	}
	got := err.Error()
	if strings.Contains(got, pw) {
		t.Fatalf("password leaked: %q", got)
	}
	if !strings.Contains(got, "office") {
		t.Fatalf("want proxy id in %q", got)
	}
}

func TestAnnotateError_Nil(t *testing.T) {
	if AnnotateError(Spec{ID: "office", Password: "x"}, nil) != nil {
		t.Fatal("nil err must stay nil")
	}
}
