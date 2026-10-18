package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Repository owns the administrators, their sessions and the audit-free
// credential rows. Reader is the optional read-only pool: it is used for
// queries that tolerate a snapshot lagging a just-committed write. Leave it nil
// and every statement goes through Database.
type Repository struct {
	Database *sql.DB
	Reader   *sql.DB
}

// reader picks the read-only pool when one is configured.
func (r *Repository) reader() *sql.DB {
	if r.Reader != nil {
		return r.Reader
	}
	return r.Database
}

const adminColumns = "id,username,display_name,role,status,password_hash,created_at,last_login_at,login_ip"

// sessionAdminColumns aliases adminColumns for the session join, so a session
// lookup stays a single statement.
var sessionAdminColumns = prefixColumns(adminColumns, "a.")

func prefixColumns(columns, prefix string) string {
	parts := strings.Split(columns, ",")
	for index := range parts {
		parts[index] = prefix + parts[index]
	}
	return strings.Join(parts, ",")
}

type scanner interface{ Scan(...any) error }

func scanAdmin(row scanner) (*Admin, error) {
	admin := &Admin{}
	err := row.Scan(&admin.ID, &admin.Username, &admin.DisplayName, &admin.Role, &admin.Status, &admin.PasswordHash, &admin.CreatedAt, &admin.LastLoginAt, &admin.LoginIP)
	return admin, err
}

func (r *Repository) Initialized(ctx context.Context) (bool, error) {
	var count int
	err := r.reader().QueryRowContext(ctx, "SELECT count(*) FROM admins WHERE role='Owner'").Scan(&count)
	return count > 0, err
}

func (r *Repository) Initialize(ctx context.Context, admin Admin, settings func(context.Context, *sql.Tx) error) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM admins").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrInitialized
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,?,?,?)", admin.ID, admin.Username, admin.DisplayName, "Owner", "active", admin.PasswordHash, admin.CreatedAt); err != nil {
		return err
	}
	if err := settings(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) FindUsername(ctx context.Context, username string) (*Admin, error) {
	return scanAdmin(r.Database.QueryRowContext(ctx, "SELECT "+adminColumns+" FROM admins WHERE username=?", username))
}
func (r *Repository) FindID(ctx context.Context, id string) (*Admin, error) {
	a, err := scanAdmin(r.Database.QueryRowContext(ctx, "SELECT "+adminColumns+" FROM admins WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return a, err
}

func (r *Repository) CreateSession(ctx context.Context, hash, adminID, ip string, expires time.Time) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(token_hash,admin_id,expires_at,created_at) VALUES(?,?,?,?)", hash, adminID, expires.UTC().Format("2006-01-02T15:04:05.000000000Z"), now); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE admins SET last_login_at=?,login_ip=? WHERE id=?", now, ip, adminID); err != nil {
		return err
	}
	return tx.Commit()
}

// Session resolves a session token and its administrator in one statement.
// The joined row is only read; a lagging snapshot can at worst keep a session
// usable for one statement after a logout, which the next request corrects.
func (r *Repository) Session(ctx context.Context, hash string) (*Admin, time.Time, error) {
	admin := &Admin{}
	var expiry string
	err := r.reader().QueryRowContext(ctx,
		"SELECT s.expires_at,"+sessionAdminColumns+" FROM sessions s JOIN admins a ON a.id=s.admin_id WHERE s.token_hash=? AND s.expires_at>? AND a.status='active'",
		hash, time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")).
		Scan(&expiry, &admin.ID, &admin.Username, &admin.DisplayName, &admin.Role, &admin.Status, &admin.PasswordHash, &admin.CreatedAt, &admin.LastLoginAt, &admin.LoginIP)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, ErrUnauthorized
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		return nil, time.Time{}, err
	}
	return admin, expires, nil
}

func (r *Repository) DeleteSession(ctx context.Context, hash string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash=?", hash)
	return err
}
func (r *Repository) RevokeSessions(ctx context.Context, id string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM sessions WHERE admin_id=?", id)
	return err
}

func (r *Repository) CreateAdmin(ctx context.Context, admin Admin) error {
	_, err := r.Database.ExecContext(ctx, "INSERT INTO admins(id,username,display_name,role,status,password_hash,created_at) VALUES(?,?,?,?,?,?,?)", admin.ID, admin.Username, admin.DisplayName, admin.Role, admin.Status, admin.PasswordHash, admin.CreatedAt)
	return err
}

func (r *Repository) List(ctx context.Context, page, size int) ([]Admin, int, error) {
	var count int
	if err := r.reader().QueryRowContext(ctx, "SELECT count(*) FROM admins").Scan(&count); err != nil {
		return nil, 0, err
	}
	rows, err := r.reader().QueryContext(ctx, "SELECT "+adminColumns+" FROM admins ORDER BY created_at,id LIMIT ? OFFSET ?", size, (page-1)*size)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	admins := []Admin{}
	for rows.Next() {
		admin, err := scanAdmin(rows)
		if err != nil {
			return nil, 0, err
		}
		admins = append(admins, *admin)
	}
	return admins, count, rows.Err()
}

func (r *Repository) UpdateAdmin(ctx context.Context, admin Admin) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE admins SET display_name=?,status=? WHERE id=? AND role='Viewer'", admin.DisplayName, admin.Status, admin.ID); err != nil {
		return err
	}
	if admin.Status == "disabled" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE admin_id=?", admin.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) DeleteAdmin(ctx context.Context, id string) error {
	_, err := r.Database.ExecContext(ctx, "DELETE FROM admins WHERE id=? AND role='Viewer'", id)
	return err
}
func (r *Repository) ChangePassword(ctx context.Context, id, hash string) error {
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE admins SET password_hash=? WHERE id=?", hash, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE admin_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// SetOwnerCredentials replaces the Owner username (only when username is
// non-empty, so an empty line keeps the current one) and password while the
// panel is stopped. The password is validated and hashed exactly like the web
// path, the password change and the session revocation commit together, and the
// Owner keeps its id, role and status.
func (r *Repository) SetOwnerCredentials(ctx context.Context, username, password string) error {
	if username != "" {
		if err := validateUsername(username); err != nil {
			return err
		}
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := r.Database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The one_owner unique index (0002_auth.sql) keeps this at most one row.
	var ownerID, ownerUsername string
	if err := tx.QueryRowContext(ctx, "SELECT id,username FROM admins WHERE role='Owner'").Scan(&ownerID, &ownerUsername); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoOwner
		}
		return err
	}
	if username != "" && username != ownerUsername {
		var taken string
		err := tx.QueryRowContext(ctx, "SELECT username FROM admins WHERE username=?", username).Scan(&taken)
		if err == nil {
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE admins SET username=? WHERE id=?", username, ownerID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE admins SET password_hash=? WHERE id=?", hash, ownerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE admin_id=?", ownerID); err != nil {
		return err
	}
	return tx.Commit()
}
