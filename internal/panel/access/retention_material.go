package access

import (
	"context"
	"database/sql"
)

// Archive validation uses the module's same reader; it never starts cleanup.
func ValidateRetentionMaterial(ctx context.Context, database *sql.DB) error {
	var count int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM access_retention_job").Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalid
	}
	var bad int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM access_retention_job WHERE id<>1 OR typeof(generation)<>'integer' OR typeof(phase)<>'integer' OR typeof(deleted_run)<>'integer' OR typeof(deleted_total)<>'integer' OR typeof(failures)<>'integer' OR typeof(policy_json)<>'text' OR length(policy_json)>1024 OR length(status)>16 OR length(error_code)>64 OR length(cutoff_at)>64 OR length(started_at)>64 OR length(updated_at)>64 OR length(last_success_at)>64").Scan(&bad); err != nil {
		return err
	}
	if bad != 0 {
		return ErrInvalid
	}
	_, err := scanRetentionJob(database.QueryRowContext(ctx, "SELECT "+retentionJobColumns+" FROM access_retention_job WHERE id=1"))
	return err
}

func ValidateSpaceMaintenanceMaterial(ctx context.Context, database *sql.DB) error {
	var count, bad int
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM access_space_maintenance").Scan(&count); err != nil {
		return err
	}
	if count != 1 {
		return ErrInvalid
	}
	if err := database.QueryRowContext(ctx, "SELECT count(*) FROM access_space_maintenance WHERE id<>1 OR typeof(attempts)<>'integer' OR typeof(unconfirmed_attempts)<>'integer' OR typeof(failures)<>'integer' OR length(status)>16 OR length(mode)>16 OR length(started_at)>64 OR length(updated_at)>64 OR length(last_success_at)>64 OR length(error_code)>64").Scan(&bad); err != nil {
		return err
	}
	if bad != 0 {
		return ErrInvalid
	}
	_, err := scanSpaceMaintenance(database.QueryRowContext(ctx, "SELECT "+spaceMaintenanceColumns+" FROM access_space_maintenance WHERE id=1"))
	return err
}
