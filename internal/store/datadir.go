package store

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvDataDir is the environment variable that overrides the default data
// location. Useful for trying the app against throwaway data, and as an escape
// hatch for anyone who keeps their home directory somewhere unusual.
const EnvDataDir = "NOTED_DATA"

// DataDirName is the folder created inside Documents.
const DataDirName = "noted-data"

// DefaultDataDir returns the folder holding noted.db and attachments/.
//
//	~/Documents/noted-data            on Linux
//	%USERPROFILE%\Documents\noted-data on Windows
//
// There is no built-in backup, so backing up has to mean copying one folder the
// user can actually find. That is why this is a visible folder under Documents
// rather than the conventional os.UserConfigDir(), which on Windows is hidden
// inside %AppData% and effectively un-copyable without being told the path.
//
// Nothing is ever placed inside the project folder, or on a network share:
// SQLite over SMB can corrupt the database.
func DefaultDataDir() (string, error) {
	if dir := os.Getenv(EnvDataDir); dir != "" {
		return filepath.Clean(dir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home directory: %w", err)
	}
	// filepath.Join, never a hardcoded separator, so the Windows build produces
	// backslashes without a second code path.
	return filepath.Join(home, "Documents", DataDirName), nil
}

// DBPath is the database file inside a data directory.
func DBPath(dataDir string) string {
	return filepath.Join(dataDir, "noted.db")
}

// AttachmentsDir is where files attached to notes are stored, under generated
// IDs rather than their original names. Windows forbids : * ? " < > | in
// filenames and treats names case-insensitively, so a generated ID sidesteps both
// that and any collision between two files called scope.csv.
func AttachmentsDir(dataDir string) string {
	return filepath.Join(dataDir, "attachments")
}

// OpenDataDir creates the data directory if needed and opens the database inside
// it. This is the entry point the app uses; Open is the lower-level form that
// tests point at a temporary file.
func OpenDataDir(dataDir string) (*Store, error) {
	// 0o700: a lab logbook can contain commercially sensitive measurements, and
	// there is no reason for other accounts on the machine to read it. Windows
	// ignores the mode and inherits the parent's ACL.
	if err := os.MkdirAll(AttachmentsDir(dataDir), 0o700); err != nil {
		return nil, fmt.Errorf("create data directory %s: %w", dataDir, err)
	}
	return Open(DBPath(dataDir))
}
