package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type agentInstallRecord struct {
	Agent       string    `json:"agent"`
	Package     string    `json:"package"`
	Manager     string    `json:"manager"`
	NPMPath     string    `json:"npm_path"`
	Prefix      string    `json:"prefix,omitempty"`
	Executable  string    `json:"executable,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
}

type agentInstallRecords struct {
	Records []agentInstallRecord `json:"records"`
}

func agentInstallRecordsPath(home string) string {
	return filepath.Join(home, ".bootagent", "agent-installs.json")
}

func (u *UseCases) loadAgentInstallRecord(agentID string) (agentInstallRecord, bool) {
	data, err := os.ReadFile(agentInstallRecordsPath(u.status.Home))
	if err != nil {
		return agentInstallRecord{}, false
	}
	var all agentInstallRecords
	if json.Unmarshal(data, &all) != nil {
		return agentInstallRecord{}, false
	}
	for _, record := range all.Records {
		if record.Agent == agentID {
			return record, true
		}
	}
	return agentInstallRecord{}, false
}

func (u *UseCases) saveAgentInstallRecord(ctx context.Context, record agentInstallRecord) error {
	path := agentInstallRecordsPath(u.status.Home)
	all := agentInstallRecords{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &all)
	}
	found := false
	for index := range all.Records {
		if all.Records[index].Agent == record.Agent {
			all.Records[index] = record
			found = true
			break
		}
	}
	if !found {
		all.Records = append(all.Records, record)
	}
	if len(all.Records) > 50 {
		all.Records = all.Records[len(all.Records)-50:]
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return err
	}
	_, err = u.filesystem.AtomicWrite(ctx, path, data, false)
	return err
}

func cleanInstallPath(value string) string { return strings.TrimSpace(filepath.Clean(value)) }

func npmPrefixForPath(npmPath string) string {
	path := cleanInstallPath(npmPath)
	if path == "" {
		return ""
	}
	// npm's executable lives in <prefix>/bin/npm on POSIX and in
	// <prefix>/npm.cmd on Windows. This is metadata only; the recorded npm
	// executable remains the authoritative command used for an approved retry.
	if filepath.Base(filepath.Dir(path)) == "bin" {
		return filepath.Dir(filepath.Dir(path))
	}
	return filepath.Dir(path)
}
