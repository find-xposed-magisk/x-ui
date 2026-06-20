package web

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/alireza0/x-ui/config"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
)

// compressibleExt lists asset extensions worth pre-compressing. Already
// compressed binary formats (woff2, images, ...) are served as-is.
var compressibleExt = map[string]bool{
	".js":   true,
	".css":  true,
	".json": true,
	".svg":  true,
	".html": true,
	".txt":  true,
	".map":  true,
}

type compressedAsset struct {
	gz []byte
	br []byte
}

// assetCache holds the brotli/gzip encoded variants of static assets so they
// are compressed only once per process instead of on every request.
var (
	assetCacheMu sync.RWMutex
	assetCache   = map[string]*compressedAsset{}
)

// embeddedAssets is the embedded FS rooted at the assets directory, so lookups
// use plain paths like "vue/vue.min.js".
var embeddedAssets, _ = fs.Sub(assetsFS, "assets")

// assetsFileSystem returns the filesystem assets are served from: the real
// directory in debug mode (so edits are picked up live) and the embedded FS in
// production.
func assetsFileSystem() fs.FS {
	if config.IsDebug() {
		return os.DirFS("web/assets")
	}
	return embeddedAssets
}

// serveAssets serves a static asset, transparently delivering a cached brotli
// or gzip encoded variant when the client supports it.
func serveAssets(c *gin.Context) {
	name := strings.TrimPrefix(path.Clean("/"+c.Param("filepath")), "/")
	if name == "" {
		c.Status(http.StatusNotFound)
		return
	}

	data, err := fs.ReadFile(assetsFileSystem(), name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	ext := strings.ToLower(path.Ext(name))
	contentType := mime.TypeByExtension(ext)
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	// Assets are versioned through the ?cur_ver query param, so a coarse
	// Last-Modified based conditional request is enough to enable 304s.
	c.Header("Last-Modified", startTime.UTC().Format(http.TimeFormat))
	if ims := c.GetHeader("If-Modified-Since"); ims != "" {
		if t, err := time.Parse(http.TimeFormat, ims); err == nil && !startTime.After(t.Add(time.Second)) {
			c.Status(http.StatusNotModified)
			return
		}
	}

	// Pre-compression is only used in production: in debug the assets are read
	// fresh from disk on every request, so caching encoded copies would serve
	// stale content after an edit.
	if !config.IsDebug() && compressibleExt[ext] && len(data) > 0 {
		c.Header("Vary", "Accept-Encoding")
		ae := c.GetHeader("Accept-Encoding")
		ca := compressedVariants(name, data)
		if strings.Contains(ae, "br") && ca.br != nil {
			c.Header("Content-Encoding", "br")
			c.Data(http.StatusOK, contentType, ca.br)
			return
		}
		if strings.Contains(ae, "gzip") && ca.gz != nil {
			c.Header("Content-Encoding", "gzip")
			c.Data(http.StatusOK, contentType, ca.gz)
			return
		}
	}

	c.Data(http.StatusOK, contentType, data)
}

// compressedVariants returns the cached brotli/gzip variants for an asset,
// building and caching them on first use.
func compressedVariants(name string, data []byte) *compressedAsset {
	assetCacheMu.RLock()
	ca := assetCache[name]
	assetCacheMu.RUnlock()
	if ca != nil {
		return ca
	}

	ca = &compressedAsset{
		gz: gzipBytes(data),
		br: brotliBytes(data),
	}

	assetCacheMu.Lock()
	assetCache[name] = ca
	assetCacheMu.Unlock()
	return ca
}

func gzipBytes(data []byte) []byte {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil
	}
	if _, err := w.Write(data); err != nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}

func brotliBytes(data []byte) []byte {
	var buf bytes.Buffer
	w := brotli.NewWriterLevel(&buf, brotli.DefaultCompression)
	if _, err := w.Write(data); err != nil {
		return nil
	}
	if err := w.Close(); err != nil {
		return nil
	}
	return buf.Bytes()
}
