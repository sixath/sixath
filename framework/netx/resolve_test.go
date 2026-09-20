package netx

import "testing"

func TestResolve_Order(t *testing.T) {
	agent := Spec{ID: "office", Type: TypeHTTP, Host: "127.0.0.1", Port: 8080, NoProxy: []string{"es.local"}}
	cat := map[string]Spec{"office": agent, "lab": {ID: "lab", Type: TypeSOCKS5, Host: "10.1.1.1", Port: 1080}}

	// off
	got, err := Resolve(Binding{Mode: ModeOff}, &agent, cat, "es.local")
	if err != nil || got != nil {
		t.Fatalf("off: %+v %v", got, err)
	}

	// tool proxy ignores no_proxy
	got, err = Resolve(Binding{Mode: ModeProxy, ProxyID: "office"}, &agent, cat, "es.local")
	if err != nil || got == nil || !got.Force {
		t.Fatalf("force: %+v %v", got, err)
	}

	// inherit + no_proxy → nil (direct)
	got, err = Resolve(Binding{Mode: ModeInherit}, &agent, cat, "es.local")
	if err != nil || got != nil {
		t.Fatalf("noproxy inherit: %+v %v", got, err)
	}

	// inherit miss no_proxy
	got, err = Resolve(Binding{Mode: ModeInherit}, &agent, cat, "gitlab.corp")
	if err != nil || got == nil || got.Spec.ID != "office" || got.Force {
		t.Fatalf("inherit: %+v", got)
	}

	// missing id
	_, err = Resolve(Binding{Mode: ModeProxy, ProxyID: "nope"}, &agent, cat, "x")
	if err == nil {
		t.Fatal("want missing proxy error")
	}
}
