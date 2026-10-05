package phone

import "testing"

func TestParseContacts(t *testing.T) {
	got, err := parseContacts("Alex = +1 (555) 123-4567, Kitchen=106\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Contact{"Alex", "15551234567"}) || got[1] != (Contact{"Kitchen", "106"}) {
		t.Fatalf("got %+v", got)
	}
	if list, err := parseContacts(""); err != nil || len(list) != 0 {
		t.Fatalf("empty: %v %v", list, err)
	}
	if _, err := parseContacts("no number here"); err == nil {
		t.Fatal("a name with no number was taken")
	}
}

func TestDialable(t *testing.T) {
	for in, want := range map[string]string{
		"+1 555-123-4567": "15551234567",
		"106":             "106",
		"*98#":            "*98#",
		" asmsaifs ":      "asmsaifs",
		"john.doe_2":      "john.doe_2",
		"bad name<>":      "badname",
	} {
		if got := dialable(in); got != want {
			t.Errorf("dialable(%q) = %q, want %q", in, got, want)
		}
	}
	list, err := parseContacts("Saif=asmsaifs, Office=106")
	if err != nil || len(list) != 2 || list[0].Number != "asmsaifs" {
		t.Errorf("got %v %v", list, err)
	}
}
