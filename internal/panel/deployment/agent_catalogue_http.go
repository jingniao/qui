package deployment

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	catalogueManifestPattern = regexp.MustCompile(`^proxy-agent/linux/(amd64|arm64)/[0-9]{1,12}/manifest\.json$`)
	catalogueArtifactPattern = regexp.MustCompile(`^artifacts/[0-9a-f]{64}\.bin$`)
)

func CatalogueHandler(directory string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}

		subPath := strings.TrimPrefix(r.URL.Path, "/agent-catalogue/")
		subPath = strings.TrimPrefix(subPath, "/")
		if !catalogueManifestPattern.MatchString(subPath) && !catalogueArtifactPattern.MatchString(subPath) {
			http.NotFound(w, r)
			return
		}

		root, err := os.OpenRoot(directory)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()

		f, err := root.Open(filepath.FromSlash(subPath))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}

		if strings.HasSuffix(subPath, ".json") {
			w.Header().Set("Content-Type", "application/json")
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}

		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = io.Copy(w, f)
		}
	})
}
