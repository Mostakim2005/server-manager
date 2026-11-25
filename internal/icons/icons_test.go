package icons

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mostakim2005/server-manager/internal/config"
	"github.com/Mostakim2005/server-manager/internal/discovery"
)

func TestResolveLocalProjectIconSkipsNoiseImages(t *testing.T) {
	proj := t.TempDir()
	// Non-icon images in attachments/characters must be ignored.
	attachDir := filepath.Join(proj, "catalog", "attachments")
	_ = os.MkdirAll(attachDir, 0o755)
	pngHdr := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}
	_ = os.WriteFile(filepath.Join(attachDir, "cover.png"), pngHdr, 0o600)

	mgr := NewManager(t.TempDir())
	svc := discovery.ServiceEntry{ID: "doujin", Name: "Doujin Manager", ProjectName: "Doujin Manager", WorkingDir: proj}
	res := mgr.Refresh([]discovery.ServiceEntry{svc}, config.AppConfig{}, nil)
	if _, ok := res["doujin"]; ok {
		t.Fatalf("expected non-icon attachment image to be ignored")
	}

	// Real favicon.svg inside web/public should be detected.
	pubDir := filepath.Join(proj, "web", "public")
	_ = os.MkdirAll(pubDir, 0o755)
	_ = os.WriteFile(filepath.Join(pubDir, "favicon.svg"), []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><circle cx="8" cy="8" r="6"/></svg>`), 0o600)

	res = mgr.Refresh([]discovery.ServiceEntry{svc}, config.AppConfig{}, nil)
	ic, ok := res["doujin"]
	if !ok || ic.Source != "local" || ic.ContentType != "image/svg+xml; charset=utf-8" {
		t.Fatalf("expected local SVG icon, got ok=%v ic=%+v", ok, ic)
	}
}

func TestLoopbackFaviconProbeAndDiskCache(t *testing.T) {
	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 1}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/favicon.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><link rel="icon" href="/favicon.png"></head></html>`))
		}
	}))
	defer ts.Close()

	cacheDir := t.TempDir()
	mgr := NewManager(cacheDir)
	svc := discovery.ServiceEntry{ID: "reg-suwayomi", Name: "Suwayomi", ProjectName: "Suwayomi", WebURL: ts.URL}

	done := make(chan ResolvedIcon, 1)
	mgr.ProbeWebFaviconAsync(svc, func(_ string, ic ResolvedIcon) {
		done <- ic
	})
	ic := <-done
	if ic.Source != "favicon" || ic.ContentType != "image/png" {
		t.Fatalf("unexpected probed favicon: %+v", ic)
	}

	// New manager should load cached favicon immediately from disk even when server is offline.
	ts.Close()
	mgr2 := NewManager(cacheDir)
	res := mgr2.Refresh([]discovery.ServiceEntry{svc}, config.AppConfig{}, nil)
	if cached, ok := res["reg-suwayomi"]; !ok || cached.Source != "favicon" {
		t.Fatalf("expected cached favicon on disk after server stopped, got ok=%v cached=%+v", ok, cached)
	}

	// Custom override replaces cached icon.
	svgFile := filepath.Join(t.TempDir(), "custom.svg")
	_ = os.WriteFile(svgFile, []byte(`<svg viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`), 0o600)
	over, err := mgr2.ApplyCustomOverride(context.Background(), svc, svgFile)
	if err != nil || over.Source != "custom" {
		t.Fatalf("expected custom override to succeed, err=%v over=%+v", err, over)
	}
}
