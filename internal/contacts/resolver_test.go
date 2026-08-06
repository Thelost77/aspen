package contacts

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func createAddressBookFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "AddressBook-v22.abcddb")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close fixture: %v", err)
		}
	}()
	statements := []string{
		`CREATE TABLE ZABCDRECORD (Z_PK INTEGER PRIMARY KEY, ZFIRSTNAME TEXT, ZLASTNAME TEXT, ZORGANIZATION TEXT)`,
		`CREATE TABLE ZABCDPHONENUMBER (ZOWNER INTEGER, ZFULLNUMBER TEXT)`,
		`CREATE TABLE ZABCDEMAILADDRESS (ZOWNER INTEGER, ZADDRESS TEXT)`,
		`INSERT INTO ZABCDRECORD VALUES (1, 'Alice', 'Fixture', '')`,
		`INSERT INTO ZABCDRECORD VALUES (2, '', '', 'Fixture Org')`,
		`INSERT INTO ZABCDRECORD VALUES (3, 'Other', 'Person', '')`,
		`INSERT INTO ZABCDPHONENUMBER VALUES (1, '+1 555 000 0006')`,
		`INSERT INTO ZABCDPHONENUMBER VALUES (2, '555-000-0000')`,
		`INSERT INTO ZABCDEMAILADDRESS VALUES (1, 'Alice@Example.Test')`,
		`INSERT INTO ZABCDEMAILADDRESS VALUES (2, 'org@example.test')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestNormalization(t *testing.T) {
	t.Parallel()
	if got := NormalizeEmail(" Alice@EXAMPLE.Test "); got != "alice@example.test" {
		t.Fatalf("NormalizeEmail() = %q", got)
	}
	if got := NormalizePhone(" +1 (555) 000-0006 "); got != "+15550000006" {
		t.Fatalf("NormalizePhone() = %q", got)
	}
	if got := NormalizePhone("alice@example.test"); got != "" {
		t.Fatalf("email normalized as phone: %q", got)
	}
	keys := PhoneKeys("+1 555")
	if len(keys) != 2 || keys[0] != "+1555" || keys[1] != "1555" {
		t.Fatalf("PhoneKeys() = %#v", keys)
	}
}

func TestResolverLoadsExactPhoneAndEmailMatches(t *testing.T) {
	t.Parallel()
	resolver, diagnostics := Load([]string{createAddressBookFixture(t)})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	tests := map[string]string{
		"+15550000006":         "Alice Fixture",
		"15550000006":          "Alice Fixture",
		"alice@example.test":   "Alice Fixture",
		"ORG@EXAMPLE.TEST":     "Fixture Org",
		"5550000000":           "Fixture Org",
		"unknown@example.test": "unknown@example.test",
		"+15550000000":         "+15550000000",
	}
	for handle, want := range tests {
		if got := resolver.Resolve(handle); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", handle, got, want)
		}
	}
}

func TestResolverRejectsAmbiguousMappings(t *testing.T) {
	t.Parallel()
	path := createAddressBookFixture(t)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO ZABCDPHONENUMBER VALUES (3, '+1 (555) 000-0006')`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	resolver, diagnostics := Load([]string{path})
	if len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	if got := resolver.Resolve("+15550000006"); got != "+15550000006" {
		t.Fatalf("ambiguous phone resolved to %q", got)
	}
}

func TestMissingOrUnsupportedSourceDoesNotBlockResolver(t *testing.T) {
	t.Parallel()
	resolver, diagnostics := Load([]string{filepath.Join(t.TempDir(), "missing.abcddb")})
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	if got := resolver.Resolve("+15550000001"); got != "+15550000001" {
		t.Fatalf("fallback = %q", got)
	}
}
