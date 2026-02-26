package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/jvanrhyn/disapyr-link/internal/model"
	"github.com/jvanrhyn/disapyr-link/internal/service"
)

// Handler holds all HTTP handler dependencies.
type Handler struct {
	svc      *service.SecretService
	log      *slog.Logger
	maxBytes int64
	tmpls    map[string]*template.Template
}

// New constructs a Handler, pre-parsing all templates from the provided FS.
func New(svc *service.SecretService, log *slog.Logger, maxBytes int64, webFS fs.FS) (*Handler, error) {
	tmpls, err := parseTemplates(webFS)
	if err != nil {
		return nil, err
	}
	return &Handler{svc: svc, log: log, maxBytes: maxBytes, tmpls: tmpls}, nil
}

// parseTemplates builds one *template.Template per page, each sharing the base layout.
func parseTemplates(webFS fs.FS) (map[string]*template.Template, error) {
	pages := []string{"index", "retrieve"}
	result := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t, err := template.ParseFS(webFS,
			"templates/base.html",
			"templates/"+page+".html",
		)
		if err != nil {
			return nil, err
		}
		result[page] = t
	}
	return result, nil
}

// render writes an HTML response by executing the named page template.
func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	t, ok := h.tmpls[name]
	if !ok {
		h.log.Error("template not found", "name", name)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		h.log.Error("render template", "name", name, "err", err)
	}
}

// ServeIndex handles GET / — the secret creation form.
func (h *Handler) ServeIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	h.render(w, "index", nil)
}

// CreateSecret handles POST / — accepts an encrypted payload and returns the retrieval token.
func (h *Handler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ciphertext string `json:"ciphertext"`
		Nonce      string `json:"nonce"`
		ExpiresIn  int    `json:"expires_in"`
	}

	// Account for base64 encoding (~4/3× expansion) plus JSON framing overhead.
	bodyLimit := h.maxBytes*4/3 + 16384
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ciphertext, err := base64.StdEncoding.DecodeString(body.Ciphertext)
	if err != nil {
		http.Error(w, "invalid ciphertext encoding", http.StatusBadRequest)
		return
	}

	nonce, err := base64.StdEncoding.DecodeString(body.Nonce)
	if err != nil {
		http.Error(w, "invalid nonce encoding", http.StatusBadRequest)
		return
	}

	token, err := h.svc.Create(r.Context(), model.CreateSecretInput{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		ExpiresIn:  body.ExpiresIn,
	})
	if err != nil {
		if errors.Is(err, service.ErrTooLarge) {
			http.Error(w, "secret too large", http.StatusRequestEntityTooLarge)
			return
		}
		h.log.ErrorContext(r.Context(), "create secret", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"token": token}); err != nil {
		h.log.ErrorContext(r.Context(), "encode create response", "err", err)
	}
}

// ServePage handles GET /s/{token} — renders the retrieve/reveal page.
func (h *Handler) ServePage(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	h.render(w, "retrieve", map[string]string{"Token": token})
}

// RevealSecret handles POST /s/{token}/reveal — atomically fetches and deletes the secret,
// returning the encrypted payload as JSON for client-side decryption.
func (h *Handler) RevealSecret(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	secret, err := h.svc.Retrieve(r.Context(), token)
	if err != nil {
		if errors.Is(err, service.ErrNotFound) {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.log.ErrorContext(r.Context(), "reveal secret", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(model.SecretPayload{
		Ciphertext: base64.StdEncoding.EncodeToString(secret.Ciphertext),
		Nonce:      base64.StdEncoding.EncodeToString(secret.Nonce),
	}); err != nil {
		h.log.ErrorContext(r.Context(), "encode reveal response", "err", err)
	}
}

// Middleware wraps an http.Handler with request logging, security headers, and panic recovery.
func Middleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Security headers
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Permissions-Policy", "interest-cohort=()")
			// Restrict script/style to same-origin; deny framing; no external resources.
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; script-src 'self'; style-src 'self' https://fonts.googleapis.com; font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none';")

			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered", "panic", rec, "path", r.URL.Path)
					http.Error(w, "internal server error", http.StatusInternalServerError)
				}
			}()

			log.Info("request", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
			next.ServeHTTP(w, r)
		})
	}
}
