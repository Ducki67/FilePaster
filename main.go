package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed static/index.html
var indexHTML embed.FS

var pageTmpl = template.Must(template.New("index").ParseFS(indexHTML, "static/index.html"))

type fileItem struct {
	Name     string
	Size     string
	ModTime  string
	IsDir    bool
	Download string
}

type pageData struct {
	Title      string
	Host       string
	Port       string
	Files      []fileItem
	BaseFolder string
	Message    string
	Protected  bool
	PeerLinks  []peerLink
	Credit     string
	HostNote   string
}

type peerLink struct {
	Host string
	URL  string
}

type appConfig struct {
	Port          string   `json:"port"`
	BindAddress   string   `json:"bind_address"`
	AdvertiseHost string   `json:"advertise_host"`
	ShareFolder   string   `json:"share_folder"`
	Password      string   `json:"password"`
	PeerHosts     []string `json:"peer_hosts"`
	OwnerCredit   string   `json:"owner_credit"`
}

func main() {
	exeDir, err := executableDir()
	if err != nil {
		log.Fatalf("resolve executable directory: %v", err)
	}

	configPath := filepath.Join(exeDir, "filepaster.config.json")
	cfg, created, err := loadOrCreateConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if created {
		log.Printf("Created config: %s", configPath)
	}

	port := firstNonEmpty(strings.TrimSpace(os.Getenv("FILEPASTER_PORT")), cfg.Port, "8080")
	bindHost := firstNonEmpty(strings.TrimSpace(os.Getenv("FILEPASTER_BIND")), cfg.BindAddress, "0.0.0.0")
	password := firstNonEmpty(strings.TrimSpace(os.Getenv("FILEPASTER_PASSWORD")), cfg.Password)

	shareSetting := firstNonEmpty(strings.TrimSpace(os.Getenv("FILEPASTER_ROOT")), cfg.ShareFolder, "shared")
	baseDir, err := resolveShareDir(exeDir, shareSetting)
	if err != nil {
		log.Fatalf("resolve share directory: %v", err)
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		log.Fatalf("create share directory: %v", err)
	}
	if err := ensureDescriptionFile(baseDir); err != nil {
		log.Fatalf("prepare description.txt: %v", err)
	}

	advertiseHost := firstNonEmpty(cfg.AdvertiseHost, preferredAdvertiseIP())
	advertiseURL := "http://" + net.JoinHostPort(advertiseHost, port)
	peerLinks := buildPeerLinks(cfg.PeerHosts, port)
	ownerCredit := firstNonEmpty(cfg.OwnerCredit, "YourName")

	changed := false
	if strings.TrimSpace(cfg.Port) == "" {
		cfg.Port = port
		changed = true
	}
	if strings.TrimSpace(cfg.BindAddress) == "" {
		cfg.BindAddress = bindHost
		changed = true
	}
	if strings.TrimSpace(cfg.AdvertiseHost) == "" {
		cfg.AdvertiseHost = advertiseHost
		changed = true
	}
	if strings.TrimSpace(cfg.ShareFolder) == "" {
		cfg.ShareFolder = "shared"
		changed = true
	}
	if cfg.PeerHosts == nil {
		cfg.PeerHosts = []string{}
		changed = true
	}
	if strings.TrimSpace(cfg.OwnerCredit) == "" {
		cfg.OwnerCredit = ownerCredit
		changed = true
	}
	if changed {
		if err := saveConfig(configPath, cfg); err != nil {
			log.Printf("warning: failed to update config defaults: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !requirePassword(w, r, password) {
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveIndex(w, r, baseDir, advertiseURL, bindHost, port, password != "", peerLinks, ownerCredit)
	})
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		if !requirePassword(w, r, password) {
			return
		}
		serveDownload(w, r, baseDir)
	})

	hostPort := net.JoinHostPort(bindHost, port)
	server := &http.Server{
		Addr:              hostPort,
		Handler:           logRequests(mux),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("FilePaster config: %s", configPath)
	log.Printf("FilePaster sharing: %s", baseDir)
	log.Printf("Share URL: %s", advertiseURL)
	log.Printf("Drop files in the shared folder, refresh the page, and download from the list")
	if password != "" {
		log.Printf("Password protection enabled via FILEPASTER_PASSWORD")
	}
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func serveIndex(w http.ResponseWriter, r *http.Request, baseDir, advertiseURL, bindHost, port string, protected bool, peerLinks []peerLink, ownerCredit string) {
	files, err := listShareableFiles(baseDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("list files: %v", err), http.StatusInternalServerError)
		return
	}

	localHost := advertiseURL
	if localHost == "" {
		localHost = urlForDisplay(bindHost, port)
	}
	hostNote := readHostDescription(baseDir)

	data := pageData{
		Title:      "FilePaster",
		Host:       localHost,
		Port:       port,
		Files:      files,
		BaseFolder: baseDir,
		Message:    "Use RadminVPN: put files in shared, run the exe, and send this URL.",
		Protected:  protected,
		PeerLinks:  peerLinks,
		Credit:     ownerCredit,
		HostNote:   hostNote,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, fmt.Sprintf("render page: %v", err), http.StatusInternalServerError)
	}
}

func serveDownload(w http.ResponseWriter, r *http.Request, baseDir string) {
	rel := strings.TrimPrefix(r.URL.Path, "/download/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}

	cleanName, ok := cleanDownloadPath(rel)
	if !ok {
		http.Error(w, "invalid file name", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(baseDir, cleanName)
	resolvedBase, err := filepath.Abs(baseDir)
	if err != nil {
		http.Error(w, "resolve base folder", http.StatusInternalServerError)
		return
	}
	resolvedFile, err := filepath.Abs(fullPath)
	if err != nil {
		http.Error(w, "resolve file path", http.StatusInternalServerError)
		return
	}
	if !strings.HasPrefix(resolvedFile, resolvedBase+string(os.PathSeparator)) && resolvedFile != resolvedBase {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	if !isDownloadableExt(filepath.Ext(info.Name())) {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(fullPath)))
	http.ServeFile(w, r, fullPath)
}

func listShareableFiles(baseDir string) ([]fileItem, error) {
	files := make([]fileItem, 0)
	err := filepath.WalkDir(baseDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == baseDir {
			return nil
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if name == filepath.Base(os.Args[0]) || name == "go.mod" || name == "filepaster.config.json" {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(baseDir, path)
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(name))
		if strings.EqualFold(filepath.ToSlash(rel), "description.txt") {
			return nil
		}

		if !isDownloadableExt(ext) {
			return nil
		}

		relSlash := filepath.ToSlash(rel)
		files = append(files, fileItem{
			Name:     relSlash,
			Size:     humanSize(info.Size()),
			ModTime:  info.ModTime().Format("2006-01-02 15:04:05"),
			IsDir:    false,
			Download: "/download/" + url.PathEscape(relSlash),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return strings.ToLower(files[i].Name) < strings.ToLower(files[j].Name)
	})
	return files, nil
}

func isDownloadableExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".rar", ".7z", ".zip", ".torrent":
		return true
	default:
		return false
	}
}

func readHostDescription(baseDir string) string {
	path := filepath.Join(baseDir, "description.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	const maxChars = 1200
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) > maxChars {
		text = string(runes[:maxChars]) + "..."
	}
	return text
}

func ensureDescriptionFile(baseDir string) error {
	path := filepath.Join(baseDir, "description.txt")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	defaultText := "Host note:\n- Put quick setup steps here for your files.\n- This text appears on the page for everyone.\n"
	return os.WriteFile(path, []byte(defaultText), 0o644)
}

func cleanDownloadPath(name string) (string, bool) {
	name, err := url.PathUnescape(name)
	if err != nil {
		return "", false
	}
	name = filepath.Clean(filepath.FromSlash(name))
	if name == "." || strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
		return "", false
	}
	return name, true
}

func humanSize(bytes int64) string {
	if bytes < 1024 {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	units := []string{"KB", "MB", "GB", "TB"}
	value := float64(bytes)
	unit := "B"
	for _, nextUnit := range units {
		value /= 1024
		unit = nextUnit
		if value < 1024 {
			break
		}
	}
	return fmt.Sprintf("%.1f %s", value, unit)
}

func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP == nil || ipnet.IP.IsLoopback() {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			return ip4.String()
		}
	}
	return "127.0.0.1"
}

func urlForDisplay(host, port string) string {
	if host == "0.0.0.0" || host == "" {
		host = localIP()
	}
	return "http://" + net.JoinHostPort(host, port)
}

func preferredAdvertiseIP() string {
	var fallback string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return localIP()
	}
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP == nil || ipnet.IP.IsLoopback() {
			continue
		}
		ip4 := ipnet.IP.To4()
		if ip4 == nil {
			continue
		}
		ip := ip4.String()
		if strings.HasPrefix(ip, "26.") {
			return ip
		}
		if fallback == "" {
			fallback = ip
		}
	}
	if fallback != "" {
		return fallback
	}
	return localIP()
}

func loadOrCreateConfig(path string) (appConfig, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var cfg appConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return appConfig{}, false, err
		}
		return cfg, false, nil
	}
	if !os.IsNotExist(err) {
		return appConfig{}, false, err
	}

	cfg := appConfig{
		Port:          "8080",
		BindAddress:   "0.0.0.0",
		AdvertiseHost: preferredAdvertiseIP(),
		ShareFolder:   "shared",
		Password:      "",
		PeerHosts:     []string{},
		OwnerCredit:   "YourName",
	}
	if err := saveConfig(path, cfg); err != nil {
		return appConfig{}, false, err
	}
	return cfg, true, nil
}

func saveConfig(path string, cfg appConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func resolveShareDir(exeDir, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		configured = "shared"
	}
	if !filepath.IsAbs(configured) {
		configured = filepath.Join(exeDir, configured)
	}
	return filepath.Abs(configured)
}

func buildPeerLinks(peerHosts []string, port string) []peerLink {
	result := make([]peerLink, 0, len(peerHosts))
	for _, raw := range peerHosts {
		host := strings.TrimSpace(raw)
		if host == "" {
			continue
		}
		if strings.Contains(host, "://") {
			parsed, err := url.Parse(host)
			if err == nil && parsed.Host != "" {
				host = parsed.Host
			}
		}
		host = strings.TrimSpace(strings.TrimSuffix(host, "/"))
		if host == "" {
			continue
		}

		fullHost := host
		if _, _, err := net.SplitHostPort(host); err != nil {
			fullHost = net.JoinHostPort(host, port)
		}

		result = append(result, peerLink{
			Host: host,
			URL:  "http://" + fullHost,
		})
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		if rec.status == http.StatusNotFound && looksLikeHostPath(r.URL.Path) {
			return
		}
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func looksLikeHostPath(path string) bool {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	ip := net.ParseIP(trimmed)
	return ip != nil && ip.To4() != nil
}

func requirePassword(w http.ResponseWriter, r *http.Request, password string) bool {
	if password == "" {
		return true
	}
	user, pass, ok := r.BasicAuth()
	if ok && user == "filepaster" && pass == password {
		return true
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="FilePaster"`)
	http.Error(w, "authentication required", http.StatusUnauthorized)
	return false
}
