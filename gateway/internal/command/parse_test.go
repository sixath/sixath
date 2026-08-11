package command

import "testing"

func TestParse_AgentSwitch(t *testing.T) {
	c, ok := Parse("/agent zone-4100-agent")
	if !ok {
		t.Fatal("expected slash command")
	}
	if c.Kind != KindAgentSwitch {
		t.Fatalf("Kind=%v want KindAgentSwitch", c.Kind)
	}
	if c.Target != "zone-4100-agent" {
		t.Fatalf("Target=%q want zone-4100-agent", c.Target)
	}

	c, ok = Parse("/AGENT my-agent")
	if !ok || c.Kind != KindAgentSwitch || c.Target != "my-agent" {
		t.Fatalf("case-insensitive switch: ok=%v cmd=%+v", ok, c)
	}

	c, ok = Parse("/agents ops-bot")
	if !ok || c.Kind != KindAgentSwitch || c.Target != "ops-bot" {
		t.Fatalf("/agents with target: ok=%v cmd=%+v", ok, c)
	}
}

func TestParse_AgentsList(t *testing.T) {
	cases := []string{"/agents", "/agent", "/agent list", "/agents list", "/Agent LIST"}
	for _, text := range cases {
		c, ok := Parse(text)
		if !ok {
			t.Fatalf("%q: expected slash command", text)
		}
		if c.Kind != KindAgentList {
			t.Fatalf("%q: Kind=%v want KindAgentList", text, c.Kind)
		}
		if c.Target != "" {
			t.Fatalf("%q: Target=%q want empty", text, c.Target)
		}
	}
}

func TestParse_New(t *testing.T) {
	c, ok := Parse("/new")
	if !ok {
		t.Fatal("expected slash command")
	}
	if c.Kind != KindNew {
		t.Fatalf("Kind=%v want KindNew", c.Kind)
	}
	c, ok = Parse("/NEW")
	if !ok || c.Kind != KindNew {
		t.Fatalf("/NEW: ok=%v cmd=%+v", ok, c)
	}
}

func TestParse_Switch(t *testing.T) {
	c, ok := Parse("/switch")
	if !ok {
		t.Fatal("expected slash command")
	}
	if c.Kind != KindSwitch {
		t.Fatalf("Kind=%v want KindSwitch", c.Kind)
	}
	if c.Target != "" {
		t.Fatalf("Target=%q want empty", c.Target)
	}

	c, ok = Parse("/SWITCH")
	if !ok || c.Kind != KindSwitch || c.Target != "" {
		t.Fatalf("/SWITCH: ok=%v cmd=%+v", ok, c)
	}

	c, ok = Parse("/switch extra-arg")
	if !ok || c.Kind != KindSwitch || c.Target != "" {
		t.Fatalf("/switch with rest ignored: ok=%v cmd=%+v", ok, c)
	}
}

func TestParse_Unbind(t *testing.T) {
	c, ok := Parse("/unbind")
	if !ok {
		t.Fatal("expected slash command")
	}
	if c.Kind != KindUnbind {
		t.Fatalf("Kind=%v want KindUnbind", c.Kind)
	}
	c, ok = Parse("  /Unbind  ")
	if !ok || c.Kind != KindUnbind {
		t.Fatalf("trimmed /Unbind: ok=%v cmd=%+v", ok, c)
	}
}

func TestParse_UnknownSlash(t *testing.T) {
	c, ok := Parse("/foo")
	if !ok {
		t.Fatal("expected slash command")
	}
	if c.Kind != KindUnknown {
		t.Fatalf("Kind=%v want KindUnknown", c.Kind)
	}
}

func TestParse_NotCommand(t *testing.T) {
	_, ok := Parse("hello")
	if ok {
		t.Fatal("expected ok=false for non-slash text")
	}
	_, ok = Parse("")
	if ok {
		t.Fatal("expected ok=false for empty text")
	}
	_, ok = Parse("hello /agent")
	if ok {
		t.Fatal("expected ok=false when text does not start with /")
	}
}
