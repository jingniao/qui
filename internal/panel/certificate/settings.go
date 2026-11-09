package certificate

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"proxycontrol/internal/platform/vault"
)

// The three system_settings rows this module owns. A missing row means "unset":
// the default CA is production and no token or account exists yet.
const (
	settingsKey = "certificate_settings"
	tokenKey    = "cloudflare_token"
	accountKey  = "acme_account"
)

// The two CAs an Owner may pick between. Staging issues untrusted certificates
// and has much wider rate limits, which is why the operations guide asks for a
// staging run before the first production one.
const (
	DirectoryProduction = "production"
	DirectoryStaging    = "staging"
)

var directoryURLs = map[string]string{
	DirectoryProduction: "https://acme-v02.api.letsencrypt.org/directory",
	DirectoryStaging:    "https://acme-staging-v02.api.letsencrypt.org/directory",
}

// DirectoryURL is the fixed ACME directory of a saved choice.
func DirectoryURL(directory string) (string, bool) {
	url, ok := directoryURLs[directory]
	return url, ok
}

// ValidDirectory reports whether a saved CA choice is one this build knows.
func ValidDirectory(directory string) bool {
	_, ok := directoryURLs[directory]
	return ok
}

// The default window, in panel-timezone hours, in which a renewed certificate may
// be deployed. A deploy restarts every core of the node, so it belongs in the
// quiet hours; the end hour is exclusive and a window may wrap midnight.
const (
	DefaultDeployStartHour = 3
	DefaultDeployEndHour   = 6
)

// ValidDeployWindow accepts two different hours of the day.
func ValidDeployWindow(start, end int) bool {
	return start >= 0 && start <= 23 && end >= 0 && end <= 23 && start != end
}

// Settings is the non-secret half of the feature: who the CA contacts on a
// problem, which CA to use, the renewal deploy window, and the revision the next
// save must present. A value stored before the window existed keeps the defaults.
type Settings struct {
	Email           string `json:"email"`
	Directory       string `json:"directory"`
	DeployStartHour int    `json:"deployStartHour"`
	DeployEndHour   int    `json:"deployEndHour"`
	Revision        int64  `json:"revision"`
	UpdatedAt       string `json:"updatedAt"`
	UpdatedBy       string `json:"updatedBy"`
}

// SettingsInput is the PUT /system/certificate-settings body. CloudflareToken is
// a pointer so that omitting it keeps the stored token while "" clears it; the
// two window hours are pointers so that omitting them keeps the stored window.
type SettingsInput struct {
	Email            string  `json:"email"`
	Directory        string  `json:"directory"`
	DeployStartHour  *int    `json:"deployStartHour"`
	DeployEndHour    *int    `json:"deployEndHour"`
	CloudflareToken  *string `json:"cloudflareToken"`
	ExpectedRevision int64   `json:"expectedRevision"`
}

// CloudflareStatus is everything the API may say about the stored token.
type CloudflareStatus struct {
	Configured bool   `json:"configured"`
	Last4      string `json:"last4"`
	VerifiedAt string `json:"verifiedAt"`
}

// SettingsView is the GET/PUT /system/certificate-settings response.
type SettingsView struct {
	Email           string           `json:"email"`
	Directory       string           `json:"directory"`
	DeployStartHour int              `json:"deployStartHour"`
	DeployEndHour   int              `json:"deployEndHour"`
	Revision        int64            `json:"revision"`
	Cloudflare      CloudflareStatus `json:"cloudflare"`
}

// Account is one ACME account. KeyPEM is the P-256 private key and never leaves
// the process unsealed.
type Account struct {
	KeyPEM    string
	URL       string
	Directory string
	Email     string
}

// tokenRecord is the stored token. The token itself is only ever inside sealed;
// last4 exists so the page can show which token is configured.
type tokenRecord struct {
	Sealed     string `json:"sealed"`
	Last4      string `json:"last4"`
	VerifiedAt string `json:"verifiedAt"`
}

type accountRecord struct {
	SealedKey  string `json:"sealedKey"`
	AccountURL string `json:"accountURL"`
	Directory  string `json:"directory"`
	Email      string `json:"email"`
}

// SaveSettingsInput is the validated write the service hands the repository.
type SaveSettingsInput struct {
	Email            string
	Directory        string
	DeployStartHour  *int
	DeployEndHour    *int
	ExpectedRevision int64
	// Token is nil to keep the stored token, empty to clear it, and a verified
	// token to replace it.
	Token      *string
	VerifiedAt string
	Actor      string
	IP         string
}

func (r *Repository) ReadSettings(ctx context.Context) (Settings, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Settings{}, err
	}
	defer tx.Rollback()
	settings, err := readSettings(ctx, tx)
	return settings, errors.Join(err, tx.Commit())
}

// readSettings decodes the stored row. An absent row is the documented default:
// production, no email, revision 0.
func readSettings(ctx context.Context, tx *sql.Tx) (Settings, error) {
	settings := Settings{Directory: DirectoryProduction, DeployStartHour: DefaultDeployStartHour, DeployEndHour: DefaultDeployEndHour}
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", settingsKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return settings, nil
	}
	if err != nil {
		return Settings{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		return Settings{}, ErrSettingsInvalid
	}
	if settings.Revision < 0 || !ValidDirectory(settings.Directory) || !ValidEmail(settings.Email) || !ValidDeployWindow(settings.DeployStartHour, settings.DeployEndHour) {
		return Settings{}, ErrSettingsInvalid
	}
	if settings.Revision > 0 {
		if _, err := time.Parse(time.RFC3339Nano, settings.UpdatedAt); err != nil {
			return Settings{}, ErrSettingsInvalid
		}
	}
	return settings, nil
}

// SaveSettings writes the email, the CA and the optional token change under one
// revision and one audit row. An unchanged save keeps the stored revision and
// writes nothing, like the other revisioned settings.
func (r *Repository) SaveSettings(ctx context.Context, input SaveSettingsInput) (Settings, bool, error) {
	email, directory := strings.TrimSpace(input.Email), strings.TrimSpace(input.Directory)
	if !ValidEmail(email) || !ValidDirectory(directory) || input.ExpectedRevision < 0 || input.Actor == "" {
		return Settings{}, false, ErrSettingsInvalid
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return Settings{}, false, err
	}
	defer tx.Rollback()
	current, err := readSettings(ctx, tx)
	if err != nil {
		return Settings{}, false, err
	}
	stored, err := r.readToken(ctx, tx)
	if err != nil {
		return Settings{}, false, err
	}
	start, end := current.DeployStartHour, current.DeployEndHour
	if input.DeployStartHour != nil {
		start = *input.DeployStartHour
	}
	if input.DeployEndHour != nil {
		end = *input.DeployEndHour
	}
	if !ValidDeployWindow(start, end) {
		return Settings{}, false, ErrSettingsInvalid
	}
	settingsUnchanged := email == current.Email && directory == current.Directory && start == current.DeployStartHour && end == current.DeployEndHour
	tokenUnchanged := input.Token == nil || *input.Token == "" && stored.Sealed == "" || *input.Token != "" && stored.token == *input.Token
	if settingsUnchanged && tokenUnchanged {
		return current, false, nil
	}
	if input.ExpectedRevision != current.Revision || current.Revision == math.MaxInt64 {
		return current, false, ErrSettingsConflict
	}
	stamp := Stamp(time.Now())
	next := Settings{Email: email, Directory: directory, DeployStartHour: start, DeployEndHour: end, Revision: current.Revision + 1, UpdatedAt: stamp, UpdatedBy: input.Actor}
	data, err := json.Marshal(next)
	if err != nil {
		return next, false, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", settingsKey, string(data)); err != nil {
		return next, false, err
	}
	if !tokenUnchanged {
		if err := r.writeToken(ctx, tx, input.Token, input.VerifiedAt); err != nil {
			return next, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admin_operations(timestamp,operator,action,target,result,ip) VALUES(?,?,'修改证书设置',?,'成功',?)", stamp, input.Actor, settingsKey, input.IP); err != nil {
		return next, false, err
	}
	if err := tx.Commit(); err != nil {
		return next, false, err
	}
	return next, true, nil
}

// storedToken is a token record with its plaintext, so an unchanged-token save
// can compare without decrypting the row twice.
type storedToken struct {
	tokenRecord
	token string
}

func (r *Repository) readToken(ctx context.Context, tx *sql.Tx) (storedToken, error) {
	var encoded string
	err := tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", tokenKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return storedToken{}, nil
	}
	if err != nil {
		return storedToken{}, err
	}
	var record tokenRecord
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return storedToken{}, ErrSettingsInvalid
	}
	sealed, err := base64.StdEncoding.DecodeString(record.Sealed)
	if err != nil {
		return storedToken{}, ErrSettingsInvalid
	}
	plaintext, err := r.Vault.Open(sealed)
	if err != nil {
		return storedToken{}, ErrSettingsInvalid
	}
	return storedToken{tokenRecord: record, token: string(plaintext)}, nil
}

// ReadCloudflare returns the stored token and what the API may say about it. The
// issuer needs the plaintext, the settings page only the record.
func (r *Repository) ReadCloudflare(ctx context.Context) (string, CloudflareStatus, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", CloudflareStatus{}, err
	}
	defer tx.Rollback()
	stored, err := r.readToken(ctx, tx)
	if err != nil {
		return "", CloudflareStatus{}, err
	}
	status := CloudflareStatus{Configured: stored.Sealed != "", Last4: stored.Last4, VerifiedAt: stored.VerifiedAt}
	return stored.token, status, tx.Commit()
}

func (r *Repository) writeToken(ctx context.Context, tx *sql.Tx, token *string, verifiedAt string) error {
	if token == nil || *token == "" {
		_, err := tx.ExecContext(ctx, "DELETE FROM system_settings WHERE key=?", tokenKey)
		return err
	}
	record, err := sealToken(r.Vault, token, verifiedAt)
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", tokenKey, string(data))
	return err
}

// last4 is the only part of a token that is ever shown.
func last4(token string) string {
	if len(token) <= 4 {
		return token
	}
	return token[len(token)-4:]
}

// sealToken is the one place a Cloudflare token is encrypted, so the vault and
// the JSON shape of the record cannot drift apart.
func sealToken(vault *vault.Vault, token *string, verifiedAt string) (tokenRecord, error) {
	sealed, err := vault.Seal([]byte(*token))
	if err != nil {
		return tokenRecord{}, err
	}
	return tokenRecord{Sealed: base64.StdEncoding.EncodeToString(sealed), Last4: last4(*token), VerifiedAt: verifiedAt}, nil
}

// ReadAccount returns the registered ACME account. An empty URL means none has
// been registered for the current directory and email yet.
func (r *Repository) ReadAccount(ctx context.Context) (Account, error) {
	tx, err := r.Database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Account{}, err
	}
	defer tx.Rollback()
	var encoded string
	err = tx.QueryRowContext(ctx, "SELECT value_json FROM system_settings WHERE key=?", accountKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, tx.Commit()
	}
	if err != nil {
		return Account{}, err
	}
	var record accountRecord
	decoder := json.NewDecoder(strings.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&record); err != nil {
		return Account{}, ErrSettingsInvalid
	}
	sealed, err := base64.StdEncoding.DecodeString(record.SealedKey)
	if err != nil {
		return Account{}, ErrSettingsInvalid
	}
	key, err := r.Vault.Open(sealed)
	if err != nil {
		return Account{}, ErrSettingsInvalid
	}
	account := Account{KeyPEM: string(key), URL: record.AccountURL, Directory: record.Directory, Email: record.Email}
	if account.URL == "" || !ValidDirectory(account.Directory) || !ValidEmail(account.Email) {
		return Account{}, ErrSettingsInvalid
	}
	return account, tx.Commit()
}

// SaveAccount seals the P-256 key and stores the account so later orders reuse
// one registration instead of creating an account per certificate.
func (r *Repository) SaveAccount(ctx context.Context, account Account) error {
	if account.KeyPEM == "" || account.URL == "" || !ValidDirectory(account.Directory) || !ValidEmail(account.Email) {
		return ErrSettingsInvalid
	}
	sealed, err := r.Vault.Seal([]byte(account.KeyPEM))
	if err != nil {
		return err
	}
	data, err := json.Marshal(accountRecord{SealedKey: base64.StdEncoding.EncodeToString(sealed), AccountURL: account.URL, Directory: account.Directory, Email: account.Email})
	if err != nil {
		return err
	}
	_, err = r.Database.ExecContext(ctx, "INSERT INTO system_settings(key,value_json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json", accountKey, string(data))
	return err
}

// accountFor is the stored account when it belongs to the saved directory and
// email, and an empty account otherwise: a changed CA or contact needs a new
// registration, which the issuer performs on the next run.
func accountFor(settings Settings, stored Account) Account {
	if stored.URL == "" || stored.Directory != settings.Directory || stored.Email != settings.Email {
		return Account{}
	}
	return stored
}
