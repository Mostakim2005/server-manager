package discovery

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Mostakim2005/server-manager/internal/config"
)

func applyGroupingAndDeduplication(entries []ServiceEntry, cfg config.AppConfig) {
	ungrouped := make(map[string]bool, len(cfg.UngroupedIDs))
	for _, id := range cfg.UngroupedIDs {
		if t := strings.TrimSpace(id); t != "" {
			ungrouped[t] = true
		}
	}

	markDuplicateWrappers(entries, ungrouped)
	assignVariantLabels(entries)

	// Collect non-utility, non-custom, non-registry, non-hidden entries by project slug.
	byProject := make(map[string][]int)
	for i := range entries {
		e := &entries[i]
		if e.IsUtility || e.IsCustom || e.SourceType == "registry" || e.HiddenDuplicate {
			continue
		}
		slug := slugify(e.ProjectName)
		if slug == "" {
			continue
		}
		byProject[slug] = append(byProject[slug], i)
	}

	usedGroupKeys := map[string]bool{}
	for projSlug, idxs := range byProject {
		if len(idxs) < 2 {
			continue
		}
		parent := make([]int, len(idxs))
		for k := range parent {
			parent[k] = k
		}
		var find func(int) int
		find = func(x int) int {
			if parent[x] != x {
				parent[x] = find(parent[x])
			}
			return parent[x]
		}
		union := func(a, b int) {
			ra, rb := find(a), find(b)
			if ra != rb {
				parent[rb] = ra
			}
		}

		for a := 0; a < len(idxs); a++ {
			for b := a + 1; b < len(idxs); b++ {
				if shouldAutoGroup(entries[idxs[a]], entries[idxs[b]]) {
					union(a, b)
				}
			}
		}

		comps := make(map[int][]int)
		for k, entryIdx := range idxs {
			r := find(k)
			comps[r] = append(comps[r], entryIdx)
		}

		for _, members := range comps {
			if len(members) < 2 {
				continue
			}
			baseKey := "proj-" + projSlug
			gKey := baseKey
			n := 2
			for usedGroupKeys[gKey] {
				gKey = fmt.Sprintf("%s-%d", baseKey, n)
				n++
			}
			usedGroupKeys[gKey] = true

			gName := entries[members[0]].ProjectName
			for _, idx := range members {
				entries[idx].AutoGroupKey = gKey
				if !ungrouped[entries[idx].ID] {
					entries[idx].GroupKey = gKey
					entries[idx].GroupName = gName
				}
			}
		}
	}

	// Build display names for existing groups (so manual merges inherit a readable GroupName).
	groupNames := make(map[string]string)
	for _, e := range entries {
		if e.GroupKey != "" && e.GroupName != "" {
			if _, ok := groupNames[e.GroupKey]; !ok {
				groupNames[e.GroupKey] = e.GroupName
			}
		}
	}
	for id, target := range cfg.ManualGroups {
		if target = strings.TrimSpace(target); target != "" {
			if _, ok := groupNames[target]; !ok {
				// Prefer the name of a non-merged member if this key matches an entry ID or project slug.
				for _, e := range entries {
					if !ungrouped[e.ID] && (e.ID == strings.TrimPrefix(target, "custom-grp-") || ("proj-"+slugify(e.ProjectName)) == target) {
						groupNames[target] = firstNonEmpty(e.ProjectName, e.Name)
						break
					}
				}
			}
			_ = id
		}
	}

	for i := range entries {
		e := &entries[i]
		if target := strings.TrimSpace(cfg.ManualGroups[e.ID]); target != "" && !ungrouped[e.ID] {
			e.GroupKey = target
			if gName := groupNames[target]; gName != "" {
				e.GroupName = gName
			} else {
				e.GroupName = prettyKey(strings.TrimPrefix(strings.TrimPrefix(target, "proj-"), "custom-grp-"))
			}
		}
	}

	// Only surface GroupKey when at least 2 visible entries share it; preserve AutoGroupKey for Re-join.
	counts := make(map[string]int)
	for _, e := range entries {
		if e.GroupKey != "" && !e.HiddenDuplicate {
			counts[e.GroupKey]++
		}
	}
	for i := range entries {
		if entries[i].GroupKey != "" && counts[entries[i].GroupKey] < 2 {
			entries[i].GroupKey = ""
			entries[i].GroupName = ""
		}
	}
}

func markDuplicateWrappers(entries []ServiceEntry, ungrouped map[string]bool) {
	contents := make(map[int]string)
	readLower := func(idx int) string {
		if c, ok := contents[idx]; ok {
			return c
		}
		raw, err := os.ReadFile(entries[idx].ScriptPath)
		if err != nil {
			contents[idx] = ""
			return ""
		}
		s := strings.ToLower(string(raw))
		contents[idx] = s
		return s
	}

	for i := range entries {
		a := &entries[i]
		if a.IsUtility || a.IsCustom || a.SourceType == "registry" || a.ScriptPath == "" {
			continue
		}
		if a.SourceType != "bat" && a.SourceType != "cmd" {
			continue
		}
		dirA := strings.ToLower(filepath.Clean(filepath.Dir(a.ScriptPath)))
		stemA := strings.ToLower(strings.TrimSuffix(filepath.Base(a.ScriptPath), filepath.Ext(a.ScriptPath)))

		for j := range entries {
			if i == j {
				continue
			}
			b := &entries[j]
			if b.IsUtility || b.IsCustom || b.SourceType == "registry" || b.ScriptPath == "" || b.SourceType != "ps1" {
				continue
			}
			if ungrouped[b.ID] {
				continue
			}
			dirB := strings.ToLower(filepath.Clean(filepath.Dir(b.ScriptPath)))
			if dirA != dirB {
				continue
			}

			baseB := strings.ToLower(filepath.Base(b.ScriptPath))
			stemB := strings.ToLower(strings.TrimSuffix(baseB, filepath.Ext(baseB)))

			// Case 1: .bat/.cmd directly wraps the sibling .ps1 file.
			if strings.Contains(readLower(i), baseB) {
				b.HiddenDuplicate = true
				continue
			}
			// Case 2: Same-folder, same-stem twin (e.g. start.bat + start.ps1) sharing ports or entrypoint.
			if stemA == stemB && (portsOverlap(a.Ports, b.Ports) || shareCommandSignature(*a, *b)) {
				b.HiddenDuplicate = true
			}
		}
	}
}

func shouldAutoGroup(a, b ServiceEntry) bool {
	if portsOverlap(a.Ports, b.Ports) {
		return true
	}
	dirA := strings.ToLower(filepath.Clean(a.WorkingDir))
	dirB := strings.ToLower(filepath.Clean(b.WorkingDir))
	if dirA != "" && dirA == dirB && (compatibleAppCategories(a, b) || isElectronWrapperPair(a, b)) {
		return true
	}
	if a.Category == "server" && b.Category == "server" && shareCommandSignature(a, b) {
		return true
	}
	return false
}

func isElectronWrapperPair(a, b ServiceEntry) bool {
	var serverEntry, desktopEntry *ServiceEntry
	if a.Category == "server" && b.Category == "desktop" {
		serverEntry, desktopEntry = &a, &b
	} else if b.Category == "server" && a.Category == "desktop" {
		serverEntry, desktopEntry = &b, &a
	} else {
		return false
	}
	if !strings.Contains(strings.ToLower(desktopEntry.Runtime), "electron") {
		return false
	}
	for _, rel := range []string{"package.json", "main.js", filepath.Join("desktop", "package.json"), filepath.Join("desktop", "main.js")} {
		if st, err := os.Stat(filepath.Join(serverEntry.WorkingDir, rel)); err == nil && !st.IsDir() {
			return true
		}
	}
	return false
}

func compatibleAppCategories(a, b ServiceEntry) bool {
	if a.Category == "server" && b.Category == "server" {
		return true
	}
	return a.Category == b.Category && runtimeFamily(a.Runtime) == runtimeFamily(b.Runtime)
}

func runtimeFamily(rt string) string {
	l := strings.ToLower(rt)
	switch {
	case strings.Contains(l, "python"):
		return "python"
	case strings.Contains(l, "node"):
		return "node"
	case strings.Contains(l, "electron"):
		return "electron"
	case strings.Contains(l, "powershell"):
		return "powershell"
	default:
		return l
	}
}

func shareCommandSignature(a, b ServiceEntry) bool {
	sigA := commandSignature(a)
	sigB := commandSignature(b)
	return sigA != "" && sigA == sigB
}

func commandSignature(e ServiceEntry) string {
	txt := strings.ToLower(e.StartCmd)
	if e.ScriptPath != "" {
		if raw, err := os.ReadFile(e.ScriptPath); err == nil {
			txt = strings.ToLower(string(raw))
		}
	}
	for _, token := range []string{"deeptutor_cli.main", "app.main:app", "server.js", "uvicorn", "vite", "control_panel.pyw"} {
		if strings.Contains(txt, token) {
			return token
		}
	}
	return ""
}

func portsOverlap(a, b []int) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[int]bool, len(a))
	for _, p := range a {
		set[p] = true
	}
	for _, p := range b {
		if set[p] {
			return true
		}
	}
	return false
}

func assignVariantLabels(entries []ServiceEntry) {
	for i := range entries {
		e := &entries[i]
		label := e.Name
		if e.ScriptPath != "" {
			label = relativeScriptDisplay(e.ProjectName, e.ScriptPath)
		} else if e.SourceType == "custom" {
			label = e.Name + " (Custom)"
		} else if e.SourceType == "registry" {
			label = e.Name + " (Registry)"
		}

		var hints []string
		if len(e.Ports) > 0 {
			ps := make([]string, len(e.Ports))
			for k, p := range e.Ports {
				ps[k] = ":" + strconv.Itoa(p)
			}
			hints = append(hints, strings.Join(ps, ","))
		}
		if e.BindHost == "0.0.0.0" && !strings.Contains(strings.ToLower(label), "lan") {
			hints = append(hints, "LAN")
		}
		if e.MissingDeps {
			hints = append(hints, "Missing deps")
		}
		if len(hints) > 0 {
			label = fmt.Sprintf("%s (%s)", label, strings.Join(hints, " · "))
		}
		e.VariantLabel = label
	}
}

func relativeScriptDisplay(projectName, scriptPath string) string {
	clean := filepath.Clean(scriptPath)
	parts := strings.Split(clean, string(filepath.Separator))
	for i, part := range parts {
		if strings.EqualFold(part, projectName) && i+1 < len(parts) {
			return filepath.ToSlash(filepath.Join(parts[i+1:]...))
		}
	}
	return filepath.Base(clean)
}
