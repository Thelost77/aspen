package contacts

import (
	"os"
	"testing"
)

func TestActualContactsReadOnlySmoke(t *testing.T) {
	if os.Getenv("ASPEN_CONTACTS_INTEGRATION") == "" {
		t.Skip("set ASPEN_CONTACTS_INTEGRATION=1 to run the read-only Contacts smoke test")
	}
	resolver, diagnostics := LoadDefault()
	if resolver == nil {
		t.Fatal("resolver is nil")
	}
	if len(diagnostics) > 0 {
		t.Fatalf("could not read all Address Book sources: %v", diagnostics)
	}
}
