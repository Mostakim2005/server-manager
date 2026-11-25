package webui

import (
	"io/fs"
	"testing"
)

func TestEmbeddedStaticAssets(t *testing.T) {
	assets, err := Assets()
	if err != nil {
		t.Fatalf("Assets() failed: %v", err)
	}
	for _, name := range []string{"index.html", "app.css", "app.js", "logo.svg"} {
		b, rerr := fs.ReadFile(assets, name)
		if rerr != nil || len(b) == 0 {
			t.Fatalf("missing or empty embedded asset %s: %v", name, rerr)
		}
	}
}
