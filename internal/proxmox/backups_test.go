package proxmox

import (
	"encoding/json"
	"testing"
)

func TestBackupJobDecoding(t *testing.T) {
	var jobs []BackupJob
	data := `[
		{"id":"a","all":1,"prune-backups":{"keep-daily":"7","keep-weekly":4},"next-run":1790000000},
		{"id":"b","enabled":0,"prune-backups":"keep-daily=3,keep-monthly=2"}
	]`
	if err := json.Unmarshal([]byte(data), &jobs); err != nil {
		t.Fatal(err)
	}

	first, second := jobs[0], jobs[1]
	if !bool(first.All) || !first.IsEnabled() || first.Retention()["keep-daily"] != 7 || first.Retention()["keep-weekly"] != 4 {
		t.Errorf("first = %+v, retention %v", first, first.Retention())
	}
	if second.IsEnabled() || second.Retention()["keep-monthly"] != 2 {
		t.Errorf("second = %+v, retention %v", second, second.Retention())
	}
}
