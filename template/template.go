package template

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tmontenegrop/kit-microsaas/config"
	"github.com/tmontenegrop/kit-microsaas/csrf"
)

type TemplateData struct {
	Title          string
	Data           interface{}
	FullWidth      bool
	HideNav        bool
	CSRFToken      string
	IdempotencyKey string
}

type Engine struct {
	mu           sync.RWMutex
	templates    map[string]*template.Template
	templatesRaw map[string]*template.Template
	funcs        template.FuncMap
	viewsDirs    []string
	reload       bool
	production   bool
}

func New(cfg config.Config, viewsDirs ...string) *Engine {
	e := &Engine{
		templates:    make(map[string]*template.Template),
		templatesRaw: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"upper": strings.ToUpper,
			"lower": strings.ToLower,
			"add":   func(a, b int) int { return a + b },
			"sub":   func(a, b int) int { return a - b },
			"seq": func(start, end int) []int {
				n := end - start + 1
				if n < 0 {
					n = 0
				}
				result := make([]int, n)
				for i := range result {
					result[i] = start + i
				}
				return result
			},
			"formatCLP": func(v interface{}) string {
				var f float64
				switch val := v.(type) {
				case float64:
					f = val
				case string:
					_, _ = fmt.Sscanf(val, "%f", &f)
				case int:
					f = float64(val)
				case int64:
					f = float64(val)
				default:
					return "$0"
				}
				return "$" + numberWithCommas(fmt.Sprintf("%.0f", f))
			},
		},
		viewsDirs:  viewsDirs,
		reload:     cfg.Env == "development",
		production: cfg.IsProduction(),
	}
	e.loadTemplates()
	return e
}

func (e *Engine) Render(w http.ResponseWriter, r *http.Request, name string, data TemplateData) {
	if e.reload {
		e.mu.Lock()
		e.loadTemplates()
		e.mu.Unlock()
	}

	e.mu.RLock()
	tmpl, ok := e.templates[name]
	e.mu.RUnlock()

	data.CSRFToken = csrf.TokenFromContext(r.Context())

	if !ok {
		if e.production {
			http.Error(w, "Error interno del servidor", http.StatusInternalServerError)
		} else {
			http.Error(w, "template no encontrado: "+name, http.StatusInternalServerError)
		}
		return
	}

	err := tmpl.Execute(w, data)
	if err != nil {
		if e.production {
			http.Error(w, "Error interno del servidor", http.StatusInternalServerError)
		} else {
			http.Error(w, "error al renderizar template: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

func (e *Engine) RenderFragment(w http.ResponseWriter, name string, data interface{}) {
	if e.reload {
		e.mu.Lock()
		e.loadTemplates()
		e.mu.Unlock()
	}

	e.mu.RLock()
	tmpl, ok := e.templatesRaw[name]
	e.mu.RUnlock()

	if !ok {
		if e.production {
			http.Error(w, "Error interno del servidor", http.StatusInternalServerError)
		} else {
			http.Error(w, "fragment no encontrado: "+name, http.StatusInternalServerError)
		}
		return
	}

	err := tmpl.Execute(w, data)
	if err != nil {
		if e.production {
			http.Error(w, "Error interno del servidor", http.StatusInternalServerError)
		} else {
			http.Error(w, "error al renderizar fragment: "+err.Error(), http.StatusInternalServerError)
		}
	}
}

func (e *Engine) RenderString(name string, data interface{}) (string, error) {
	if e.reload {
		e.mu.Lock()
		e.loadTemplates()
		e.mu.Unlock()
	}

	e.mu.RLock()
	tmpl, ok := e.templatesRaw[name]
	e.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("template no encontrado: %s", name)
	}

	var buf strings.Builder
	err := tmpl.Execute(&buf, data)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (e *Engine) loadTemplates() {
	e.templates = make(map[string]*template.Template)
	e.templatesRaw = make(map[string]*template.Template)

	layoutFile := e.findLayout()
	pages := e.collectPages()
	partials := e.collectPartials()

	partialContent := ""
	for _, p := range partials {
		b, err := os.ReadFile(p)
		if err == nil {
			content := string(b)
			if strings.Contains(content, "{{define") {
				partialContent += "\n" + content
			}
		}
	}

	if layoutFile != "" {
		layoutBytes, err := os.ReadFile(layoutFile)
		if err == nil {
			layoutContent := string(layoutBytes)
			for name, pageFile := range pages {
				pageBytes, err := os.ReadFile(pageFile)
				if err != nil {
					continue
				}
				combined := layoutContent + partialContent + "\n" + string(pageBytes)
				tmpl, err := template.New(name).Funcs(e.funcs).Parse(combined)
				if err != nil {
					slog.Error("[TEMPLATE] Error parsing", "name", name, "error", err)
					continue
				}
				e.templates[name] = tmpl
			}
		}
	}

	for name, pageFile := range pages {
		fileBytes, err := os.ReadFile(pageFile)
		if err != nil {
			continue
		}
		combined := string(fileBytes) + partialContent
		tmpl, err := template.New(name).Funcs(e.funcs).Parse(combined)
		if err != nil {
			slog.Error("[TEMPLATE] Error parsing raw", "name", name, "error", err)
			continue
		}
		e.templatesRaw[name] = tmpl
	}

	for _, p := range partials {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		rel := "partials/" + filepath.Base(p)
		name := strings.TrimSuffix(rel, ".html")
		tmpl, err := template.New(name).Funcs(e.funcs).Parse(string(b))
		if err != nil {
			slog.Error("[TEMPLATE] Error parsing partial", "name", name, "error", err)
			continue
		}
		e.templatesRaw[name] = tmpl
	}
}

func (e *Engine) findLayout() string {
	for _, dir := range e.viewsDirs {
		candidate := filepath.Join(dir, "layout.html")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func (e *Engine) collectPages() map[string]string {
	pages := make(map[string]string)
	for _, dir := range e.viewsDirs {
		_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(info.Name(), ".html") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			if rel == "layout.html" {
				return nil
			}
			if strings.HasPrefix(strings.ReplaceAll(rel, "\\", "/"), "partials/") {
				return nil
			}
			name := strings.TrimSuffix(rel, ".html")
			name = strings.ReplaceAll(name, "\\", "/")
			if _, exists := pages[name]; !exists {
				pages[name] = path
			}
			return nil
		})
	}
	return pages
}

func (e *Engine) collectPartials() []string {
	var partials []string
	seen := map[string]bool{}
	for _, dir := range e.viewsDirs {
		partialDir := filepath.Join(dir, "partials")
		entries, err := os.ReadDir(partialDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".html") && !seen[entry.Name()] {
				seen[entry.Name()] = true
				partials = append(partials, filepath.Join(partialDir, entry.Name()))
			}
		}
	}
	return partials
}

func numberWithCommas(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	var parts []string
	for i := n; i > 0; i -= 3 {
		start := i - 3
		if start < 0 {
			start = 0
		}
		parts = append([]string{s[start:i]}, parts...)
	}
	return strings.Join(parts, ".")
}
