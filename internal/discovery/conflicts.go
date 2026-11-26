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
	for idx := range entries {
		entries[idx].Conflicts = nil
		if entries[idx].IsUtility {
			continue
		}
		for _, pt := range entries[idx].Ports {
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
		if e.MissingDeps && e.MissingDepsReason != "" {
			e.Conflicts = append(e.Conflicts, ConflictWarning{
				Severity: "warning",
				Code:     "MISSING_DEPS",
				Message:  e.MissingDepsReason,
			})
		}
		if e.BindHost == "0.0.0.0" {
			e.Conflicts = append(e.Conflicts, ConflictWarning{
				Severity: "info",
				Code:     "LAN_BIND_0000",
				Message:  "Binds 0.0.0.0 (accessible over Wi-Fi/LAN instead of loopback 127.0.0.1 only).",
			})
		}
		projSlug := slugify(e.ProjectName)
		if expectedPorts := registryPorts[projSlug]; len(expectedPorts) > 0 && len(e.Ports) > 0 {
			matched := false
			for _, pt := range e.Ports {
				if containsPort(expectedPorts, pt) {
					matched = true
					break
				}
			}
			if !matched {
				msg := fmt.Sprintf("Script port %v differs from ecosystem-registry.json expected port :%d.", e.Ports, expectedPorts[0])
				if len(expectedPorts) > 1 {
					msg = fmt.Sprintf("Script port %v differs from ecosystem-registry.json expected ports [%s].", e.Ports, formatExpectedPorts(expectedPorts))
				}
				w := ConflictWarning{
					Severity: "warning",
					Code:     "REGISTRY_PORT_MISMATCH",
					Message:  msg,
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

func extractRegistryPortMap(reg *config.EcosystemRegistry) map[string][]int {
	res := make(map[string][]int)
	if reg == nil {
		return res
	}
	readMap := func(m map[string]map[string]any) {
		for k, meta := range m {
			pts := extractOwnRegistryPorts(meta)
			if len(pts) > 0 {
				appendRegistrySlugPorts(res, k, pts)
			}
		}
	}
	readMap(reg.Apps)
	readMap(reg.Services)
	return res
}

func extractOwnRegistryPorts(meta map[string]any) []int {
	var out []int
	addPort := func(p int) {
		if p > 0 && p <= 65535 && !containsPort(out, p) {
			out = append(out, p)
		}
	}
	for _, numKey := range []string{"port"} {
		if pFloat, ok := meta[numKey].(float64); ok && pFloat > 0 {
			addPort(int(pFloat))
		}
	}
	for _, urlKey := range []string{"webUrl", "url", "mcp"} {
		if u, ok := meta[urlKey].(string); ok && u != "" {
			for _, pt := range extractPorts(u) {
				addPort(pt)
			}
		}
	}
	for _, auxKey := range []string{"visualEditorPort", "lanQrPort"} {
		if pFloat, ok := meta[auxKey].(float64); ok && pFloat > 0 {
			addPort(int(pFloat))
		}
	}
	return out
}

func appendRegistrySlugPorts(dst map[string][]int, rawKey string, ports []int) {
	for _, slug := range []string{slugify(prettyKey(rawKey)), slugify(rawKey)} {
		if slug == "" {
			continue
		}
		cur := dst[slug]
		for _, pt := range ports {
			if !containsPort(cur, pt) {
				cur = append(cur, pt)
			}
		}
		dst[slug] = cur
	}
}

func containsPort(ports []int, target int) bool {
	for _, p := range ports {
		if p == target {
			return true
		}
	}
	return false
}

func formatExpectedPorts(ports []int) string {
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprintf(":%d", p)
	}
	return strings.Join(parts, ", ")
}
