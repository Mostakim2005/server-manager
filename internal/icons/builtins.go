package icons

import (
	"strings"

	"github.com/Mostakim2005/server-manager/internal/discovery"
)

const svgDatabaseMinimal = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40">
  <rect x="2" y="2" width="36" height="36" rx="10" fill="#0f766e"/>
  <ellipse cx="20" cy="12" rx="10" ry="3.5" fill="#99f6e4"/>
  <path d="M10 12v6c0 1.93 4.48 3.5 10 3.5s10-1.57 10-3.5v-6" fill="none" stroke="#ccfbf1" stroke-width="2.2" stroke-linejoin="round"/>
  <path d="M10 18v6c0 1.93 4.48 3.5 10 3.5s10-1.57 10-3.5v-6" fill="none" stroke="#ccfbf1" stroke-width="2.2" stroke-linejoin="round"/>
  <path d="M10 24v5c0 1.93 4.48 3.5 10 3.5s10-1.57 10-3.5v-5" fill="none" stroke="#99f6e4" stroke-width="2.2" stroke-linejoin="round"/>
</svg>`

const svgDoujinMinimal = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40">
  <rect x="2" y="2" width="36" height="36" rx="10" fill="#be123c"/>
  <rect x="9" y="8" width="22" height="24" rx="2.5" fill="#fff1f2"/>
  <path d="M12 11.5h16v6.5L12 20.5z" fill="#fb7185"/>
  <path d="M12 22.5l7.5-1.2v7.2H12z" fill="#fda4af"/>
  <path d="M21.2 21l6.8-1v8.5h-6.8z" fill="#f43f5e"/>
  <rect x="9" y="8" width="22" height="24" rx="2.5" fill="none" stroke="#881337" stroke-width="1.4"/>
</svg>`

const svgTransformerLLM = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40">
  <defs>
    <linearGradient id="llmG" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0%" stop-color="#4338ca"/>
      <stop offset="100%" stop-color="#7c3aed"/>
    </linearGradient>
  </defs>
  <rect x="2" y="2" width="36" height="36" rx="10" fill="url(#llmG)"/>
  <g stroke="#c4b5fd" stroke-width="1.7" stroke-linecap="round">
    <line x1="11" y1="12" x2="20" y2="15"/>
    <line x1="11" y1="12" x2="20" y2="25"/>
    <line x1="11" y1="20" x2="20" y2="15"/>
    <line x1="11" y1="20" x2="20" y2="25"/>
    <line x1="11" y1="28" x2="20" y2="15"/>
    <line x1="11" y1="28" x2="20" y2="25"/>
    <line x1="20" y1="15" x2="29" y2="12"/>
    <line x1="20" y1="15" x2="29" y2="20"/>
    <line x1="20" y1="25" x2="29" y2="20"/>
    <line x1="20" y1="25" x2="29" y2="28"/>
  </g>
  <g fill="#ffffff">
    <circle cx="11" cy="12" r="2.3"/>
    <circle cx="11" cy="20" r="2.3"/>
    <circle cx="11" cy="28" r="2.3"/>
    <circle cx="20" cy="15" r="2.6" fill="#fde68a"/>
    <circle cx="20" cy="25" r="2.6" fill="#fde68a"/>
    <circle cx="29" cy="12" r="2.3"/>
    <circle cx="29" cy="20" r="2.3"/>
    <circle cx="29" cy="28" r="2.3"/>
  </g>
</svg>`

const svgRAGSystem = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40">
  <rect x="2" y="2" width="36" height="36" rx="10" fill="#0369a1"/>
  <rect x="9" y="9" width="14" height="19" rx="2.5" fill="#e0f2fe"/>
  <path d="M12.5 14h7M12.5 18h7M12.5 22h4.5" stroke="#0284c7" stroke-width="2" stroke-linecap="round"/>
  <circle cx="25.5" cy="23.5" r="5.5" fill="#0c4a6e" stroke="#7dd3fc" stroke-width="2.2"/>
  <line x1="29.5" y1="27.5" x2="33" y2="31" stroke="#7dd3fc" stroke-width="2.6" stroke-linecap="round"/>
  <circle cx="25.5" cy="23.5" r="1.8" fill="#bae6fd"/>
</svg>`

const svgObsidianFallback = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40">
  <rect x="2" y="2" width="36" height="36" rx="10" fill="#1e1b4b"/>
  <polygon points="21,6 30,14 27,31 16,34 10,22 14,11" fill="#7c3aed"/>
  <polygon points="21,6 30,14 21,21 14,11" fill="#a78bfa"/>
  <polygon points="21,21 30,14 27,31" fill="#6d28d9"/>
  <polygon points="10,22 14,11 21,21 16,34" fill="#5b21b6"/>
</svg>`

const svgAnkiFallback = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 40 40">
  <rect x="2" y="2" width="36" height="36" rx="10" fill="#0284c7"/>
  <polygon points="20,7 23.6,14.8 32.1,15.8 25.8,21.6 27.5,30 20,25.8 12.5,30 14.2,21.6 7.9,15.8 16.4,14.8" fill="#ffffff" stroke="#bae6fd" stroke-width="1.4" stroke-linejoin="round"/>
</svg>`

func resolvePresetSVG(s discovery.ServiceEntry, preferredOnly bool) (ResolvedIcon, bool) {
	slug := sanitizeSlug(s.ProjectName + " " + s.Name + " " + s.ID)
	if strings.Contains(slug, "gptmoe3dexplainer") || slug == "regollama" || strings.Contains(slug, "ollama") {
		return newResolvedIcon([]byte(svgTransformerLLM), "image/svg+xml; charset=utf-8", "preset"), true
	}
	if preferredOnly {
		return ResolvedIcon{}, false
	}
	switch {
	case strings.Contains(slug, "databasemanager"):
		return newResolvedIcon([]byte(svgDatabaseMinimal), "image/svg+xml; charset=utf-8", "preset"), true
	case strings.Contains(slug, "doujinmanager"):
		return newResolvedIcon([]byte(svgDoujinMinimal), "image/svg+xml; charset=utf-8", "preset"), true
	case strings.Contains(slug, "ragmanager"):
		return newResolvedIcon([]byte(svgRAGSystem), "image/svg+xml; charset=utf-8", "preset"), true
	case strings.Contains(slug, "obsidian"):
		return newResolvedIcon([]byte(svgObsidianFallback), "image/svg+xml; charset=utf-8", "preset"), true
	case strings.Contains(slug, "anki"):
		return newResolvedIcon([]byte(svgAnkiFallback), "image/svg+xml; charset=utf-8", "preset"), true
	}
	return ResolvedIcon{}, false
}
