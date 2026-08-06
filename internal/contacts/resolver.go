package contacts

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

type Resolver struct {
	phones          map[string]string
	emails          map[string]string
	ambiguousPhones map[string]bool
	ambiguousEmails map[string]bool
}

func NewResolver() *Resolver {
	return &Resolver{
		phones:          make(map[string]string),
		emails:          make(map[string]string),
		ambiguousPhones: make(map[string]bool),
		ambiguousEmails: make(map[string]bool),
	}
}

func DefaultPaths() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(home, "Library", "Application Support", "AddressBook", "Sources", "*", "AddressBook-v22.abcddb")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func LoadDefault() (*Resolver, []error) {
	paths, err := DefaultPaths()
	if err != nil {
		return NewResolver(), []error{err}
	}
	return Load(paths)
}

func Load(paths []string) (*Resolver, []error) {
	resolver := NewResolver()
	var diagnostics []error
	for _, path := range paths {
		if err := resolver.loadDatabase(path); err != nil {
			diagnostics = append(diagnostics, err)
		}
	}
	return resolver, diagnostics
}

func (r *Resolver) Resolve(handle string) string {
	if email := NormalizeEmail(handle); email != "" {
		if name := r.emails[email]; name != "" && !r.ambiguousEmails[email] {
			return name
		}
	}
	for _, phone := range PhoneKeys(handle) {
		if name := r.phones[phone]; name != "" && !r.ambiguousPhones[phone] {
			return name
		}
	}
	return handle
}

func NormalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return ""
	}
	return email
}

func NormalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" || strings.Contains(phone, "@") {
		return ""
	}
	hasPlus := strings.HasPrefix(phone, "+")
	var digits strings.Builder
	for _, value := range phone {
		if unicode.IsDigit(value) {
			digits.WriteRune(value)
		}
	}
	if digits.Len() == 0 {
		return ""
	}
	if hasPlus {
		return "+" + digits.String()
	}
	return digits.String()
}

func PhoneKeys(phone string) []string {
	normalized := NormalizePhone(phone)
	if normalized == "" {
		return nil
	}
	keys := []string{normalized}
	if strings.HasPrefix(normalized, "+") {
		keys = append(keys, strings.TrimPrefix(normalized, "+"))
	}
	return keys
}

func (r *Resolver) loadDatabase(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve Address Book source: %w", err)
	}
	u := &url.URL{Scheme: "file", Path: absolute}
	query := u.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "query_only(1)")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return fmt.Errorf("open Address Book source: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err := db.Ping(); err != nil {
		return fmt.Errorf("read Address Book source: %w", err)
	}
	if err := r.loadPhones(db); err != nil {
		return err
	}
	if err := r.loadEmails(db); err != nil {
		return err
	}
	return nil
}

func (r *Resolver) loadPhones(db *sql.DB) error {
	rows, err := db.Query(`
SELECT
	COALESCE(record.ZFIRSTNAME, ''),
	COALESCE(record.ZLASTNAME, ''),
	COALESCE(record.ZORGANIZATION, ''),
	COALESCE(phone.ZFULLNUMBER, '')
FROM ZABCDRECORD record
JOIN ZABCDPHONENUMBER phone ON phone.ZOWNER = record.Z_PK
WHERE phone.ZFULLNUMBER IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("query Address Book phones: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var first, last, organization, phone string
		if err := rows.Scan(&first, &last, &organization, &phone); err != nil {
			return fmt.Errorf("scan Address Book phone: %w", err)
		}
		name := displayName(first, last, organization)
		for _, key := range PhoneKeys(phone) {
			addMapping(r.phones, r.ambiguousPhones, key, name)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query Address Book phones: %w", err)
	}
	return nil
}

func (r *Resolver) loadEmails(db *sql.DB) error {
	rows, err := db.Query(`
SELECT
	COALESCE(record.ZFIRSTNAME, ''),
	COALESCE(record.ZLASTNAME, ''),
	COALESCE(record.ZORGANIZATION, ''),
	COALESCE(email.ZADDRESS, '')
FROM ZABCDRECORD record
JOIN ZABCDEMAILADDRESS email ON email.ZOWNER = record.Z_PK
WHERE email.ZADDRESS IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("query Address Book emails: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var first, last, organization, email string
		if err := rows.Scan(&first, &last, &organization, &email); err != nil {
			return fmt.Errorf("scan Address Book email: %w", err)
		}
		addMapping(r.emails, r.ambiguousEmails, NormalizeEmail(email), displayName(first, last, organization))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("query Address Book emails: %w", err)
	}
	return nil
}

func addMapping(values map[string]string, ambiguous map[string]bool, key, name string) {
	if key == "" || name == "" || ambiguous[key] {
		return
	}
	if existing := values[key]; existing != "" && existing != name {
		delete(values, key)
		ambiguous[key] = true
		return
	}
	values[key] = name
}

func displayName(first, last, organization string) string {
	name := strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
	if name != "" {
		return name
	}
	return strings.TrimSpace(organization)
}
