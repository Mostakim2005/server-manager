package discovery

import (
	"fmt"
	"strings"

	"github.com/Mostakim2005/server-manager/internal/config"
)

// DetectConflicts inspects non-utility services for duplicate TCP ports,
// mismatches against ecosystem-registry.json, and 0.0.0.0 LAN bindings (Charlie 3).
func DetectConflicts(entries []ServiceEntry, reg *config.EcosystemRegistry) []ConflictWarning {
	portOwners := make(map[int][]int) // port -> indices in entries
	for idx, e := range entries {
		if e.IsUtility {
			continue
		}
		for _, pt := range e.Ports {
			portOwners[pt] = append(portOwners[pt], idx)
		}
	}

	var global []ConflictWarning
	for port, indices := range portOwners {
		if len(indices) <= 1 {
			continue
		}
		// Only flag collisions across distinct projects (multiple scripts inside the same project are alternative launch modes).
		distinctProjects := make(map[string]bool)
		names := make([]string, 0, len(indices))
		for _, idx := range indices {
			projKey := slugify(entries[idx].ProjectName)
			if !distinctProjects[projKey] {
				distinctProjects[projKey] = true
			}
			names = append(names, entries[idx].Name)
		}
		if len(distinctProjects) <= 1 {
			continue
		}
		msg := fmt.Sprintf("Port :%d is claimed by multiple launchers (%s)", port, strings.Join(names, ", "))
		w := ConflictWarning{Severity: "warning", Code: "DUPLICATE_PORT", Message: msg}
		global = append(global, w)
		for _, idx := range indices {
			entries[idx].Conflicts = append(entries[idx].Conflicts, w)
		}
	}

	// Flag 0.0.0.0 LAN exposure and registry port mismatches.
	registryPorts := extractRegistryPortMap(reg)
	for i := range entries {
		e := &entries[i]
		if e.IsUtility {
			continue
		}
		if e.BindHost == "0.0.0.0" {
			e.Conflicts = append(e.Conflicts, ConflictWarning{
				Severity: "info",
				Code:     "LAN_BIND_0000",
				Message:  "Binds 0.0.0.0 (accessible over Wi-Fi/LAN instead of loopback 127.0.0.1 only).",
			})
		}
		projSlug := slugify(e.ProjectName)
		if expected, ok := registryPorts[projSlug]; ok && len(e.Ports) > 0 {
			matched := false
			for _, pt := range e.Ports {
				if pt == expected {
					matched = true
					break
				}
			}
			if !matched {
				w := ConflictWarning{
					Severity: "warning",
					Code:     "REGISTRY_PORT_MISMATCH",
					Message:  fmt.Sprintf("Script port %v differs from ecosystem-registry.json expected port :%d.", e.Ports, expected),
				}
				e.Conflicts = append(e.Conflicts, w)
				global = append(global, ConflictWarning{
					Severity: w.Severity,
					Code:     w.Code,
					Message:  fmt.Sprintf("%s: %s", e.Name, w.Message),
				})
			}
		}
	}
	return global
}

func extractRegistryPortMap(reg *config.EcosystemRegistry) map[string]int {
	res := make(map[string]int)
	if reg == nil {
		return res
	}
	readMap := func(m map[string]map[string]any) {
		for k, meta := range m {
			slug := slugify(prettyKey(k))
			if pFloat, ok := meta["port"].(float64); ok && pFloat > 0 {
				res[slug] = int(pFloat)
				continue
			}
			u, _ := meta["webUrl"].(string)
			if u == "" {
				u, _ = meta["url"].(string)
			}
			if pts := extractPorts(u); len(pts) > 0 {
				res[slug] = pts[0]
			}
		}
	}
	readMap(reg.Services)
	readMap(reg.Apps)
	return res
}
