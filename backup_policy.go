package main

import (
	"os"
	"sort"
	"time"
)

func selectBackupsToDelete(entries []BackupEntry, policy BackupRetentionPolicy, now time.Time) []BackupEntry {
	ordered := append([]BackupEntry(nil), entries...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].CreatedAt > ordered[j].CreatedAt })
	switch policy.Mode {
	case "count":
		count := policy.Count
		if count < 1 {
			count = 30
		}
		if len(ordered) > count {
			return ordered[count:]
		}
	case "days":
		days := policy.Days
		if days < 1 {
			days = 30
		}
		cutoff := now.AddDate(0, 0, -days)
		result := make([]BackupEntry, 0)
		for _, entry := range ordered {
			if time.UnixMilli(entry.CreatedAt).Before(cutoff) {
				result = append(result, entry)
			}
		}
		return result
	case "tiered":
		keptDays := map[string]bool{}
		keptMonths := map[string]bool{}
		result := make([]BackupEntry, 0)
		for _, entry := range ordered {
			created := time.UnixMilli(entry.CreatedAt).In(now.Location())
			age := now.Sub(created)
			if age <= 7*24*time.Hour {
				continue
			}
			if age <= 30*24*time.Hour {
				key := created.Format("2006-01-02")
				if keptDays[key] {
					result = append(result, entry)
				} else {
					keptDays[key] = true
				}
				continue
			}
			key := created.Format("2006-01")
			if keptMonths[key] {
				result = append(result, entry)
			} else {
				keptMonths[key] = true
			}
		}
		return result
	}
	return nil
}

func (a *App) PruneBackups(id string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	entries, err := a.ListBackups(id)
	if err != nil {
		return err
	}
	policy := BackupRetentionPolicy{Mode: instance.BackupRetentionMode, Count: instance.BackupRetentionCount, Days: instance.BackupRetentionDays}
	for _, entry := range selectBackupsToDelete(entries, policy, time.Now()) {
		if err := os.RemoveAll(entry.Path); err != nil {
			return err
		}
	}
	return nil
}
