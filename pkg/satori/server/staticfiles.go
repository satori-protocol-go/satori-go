package server

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type staticFilesMount struct {
	routePath      string
	fs             *staticFileSystem
	fileServer     http.Handler
	html           bool
	has404Template bool
}

type staticMountKind string

const (
	staticMountKindFile      staticMountKind = "file"
	staticMountKindDirectory staticMountKind = "directory"
)

func newStaticFilesMountFromFile(routePath string, filePath string) (*staticFilesMount, error) {
	routePath = normalizeStaticRoutePath(routePath)
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("mount file expects a file path, got directory: %s (use MountDir)", filePath)
	}
	rootDir := filepath.Dir(filePath)
	html := strings.EqualFold(filepath.Ext(filePath), ".html")
	return newStaticFilesMount(routePath, rootDir, html), nil
}

func newStaticFilesMountFromDirectory(routePath string, directoryPath string, html bool) (*staticFilesMount, error) {
	routePath = normalizeStaticRoutePath(routePath)
	info, err := os.Stat(directoryPath)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("mount directory expects a directory path, got file: %s (use MountFile)", directoryPath)
	}
	return newStaticFilesMount(routePath, directoryPath, html), nil
}

func newStaticFilesMount(routePath string, rootDir string, html bool) *staticFilesMount {
	filesystem := &staticFileSystem{
		root: http.Dir(rootDir),
		html: html,
	}
	rawFileServer := http.FileServer(filesystem)
	fileServer := rawFileServer
	if routePath != "/" {
		fileServer = http.StripPrefix(routePath, rawFileServer)
	}

	has404Template := false
	if file404, openErr := filesystem.Open("/404.html"); openErr == nil {
		has404Template = true
		_ = file404.Close()
	}

	return &staticFilesMount{
		routePath:      routePath,
		fs:             filesystem,
		fileServer:     fileServer,
		html:           html,
		has404Template: has404Template,
	}
}

func (m *staticFilesMount) RoutePath() string {
	return m.routePath
}

func (m *staticFilesMount) Pattern() string {
	if m.routePath == "/" {
		return "/*"
	}
	return m.routePath + "/*"
}

func (m *staticFilesMount) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet, http.MethodHead:
	default:
		writeMethodNotAllowed(w)
		return
	}

	prepared, staticPath, ok := m.prepareRequestPath(request)
	if !ok {
		http.NotFound(w, request)
		return
	}

	if !m.pathExists(staticPath) {
		if m.html && m.has404Template {
			fallback := cloneRequestWithPath(prepared, m.joinRoutePath("/404.html"))
			w.WriteHeader(http.StatusNotFound)
			m.fileServer.ServeHTTP(w, fallback)
			return
		}
		http.NotFound(w, prepared)
		return
	}

	m.fileServer.ServeHTTP(w, prepared)
}

func (m *staticFilesMount) prepareRequestPath(request *http.Request) (*http.Request, string, bool) {
	if request == nil || request.URL == nil {
		return request, "", false
	}

	currentPath := request.URL.Path
	if currentPath == "" {
		currentPath = "/"
	}

	if m.routePath == "/" {
		if !strings.HasPrefix(currentPath, "/") {
			currentPath = "/" + currentPath
		}
		return cloneRequestWithPath(request, currentPath), currentPath, true
	}

	if !strings.HasPrefix(currentPath, m.routePath) {
		return request, "", false
	}

	if len(currentPath) > len(m.routePath) {
		next := currentPath[len(m.routePath)]
		if next != '/' {
			return request, "", false
		}
	}

	if currentPath == m.routePath {
		currentPath = m.routePath + "/"
	}
	staticPath := strings.TrimPrefix(currentPath, m.routePath)
	if staticPath == "" {
		staticPath = "/"
	}
	if !strings.HasPrefix(staticPath, "/") {
		staticPath = "/" + staticPath
	}

	prepared := cloneRequestWithPath(request, currentPath)
	return prepared, staticPath, true
}

func (m *staticFilesMount) pathExists(staticPath string) bool {
	file, err := m.fs.Open(staticPath)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

func (m *staticFilesMount) joinRoutePath(staticPath string) string {
	if !strings.HasPrefix(staticPath, "/") {
		staticPath = "/" + staticPath
	}
	if m.routePath == "/" {
		return staticPath
	}
	return strings.TrimSuffix(m.routePath, "/") + staticPath
}

type staticFileSystem struct {
	root http.FileSystem
	html bool
}

func (s *staticFileSystem) Open(name string) (http.File, error) {
	clean := path.Clean("/" + strings.TrimPrefix(name, "/"))
	file, err := s.root.Open(clean)
	if err != nil {
		return nil, err
	}

	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !info.IsDir() {
		return file, nil
	}

	_ = file.Close()
	if !s.html {
		return nil, os.ErrNotExist
	}

	indexPath := path.Join(clean, "index.html")
	indexFile, indexErr := s.root.Open(indexPath)
	if indexErr != nil {
		return nil, os.ErrNotExist
	}
	return indexFile, nil
}

func normalizeStaticRoutePath(routePath string) string {
	routePath = strings.TrimSpace(routePath)
	if routePath == "" {
		return "/"
	}
	if !strings.HasPrefix(routePath, "/") {
		routePath = "/" + routePath
	}
	if routePath != "/" {
		routePath = strings.TrimSuffix(routePath, "/")
	}
	if routePath == "" {
		return "/"
	}
	return routePath
}

func cloneRequestWithPath(request *http.Request, pathValue string) *http.Request {
	cloned := request.Clone(request.Context())
	urlValue := *request.URL
	urlValue.Path = pathValue
	cloned.URL = &urlValue
	return cloned
}
