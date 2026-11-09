package httpapi

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"sweetshop/internal/application"
	"sweetshop/internal/domain"
	"sweetshop/internal/media"
)

// Managed product media (ADR 019): public delivery at /media/{id} and the
// staff media library under /api/admin/v1/media. Uploads are
// multipart/form-data with exactly one part named "file"; its file name and
// declared type are ignored, the bytes decide.

// multipartOverhead is what the request may carry beyond the file itself:
// boundaries and part headers.
const multipartOverhead = 64 << 10

// WithMedia enables managed media: the public /media route always, and the
// staff media routes when the staff API exists.
func WithMedia(library *application.Media) Option {
	return func(s *Server) { s.media = library }
}

// registerMediaRoutes adds public delivery. /media is a resource path like
// /assets, not an API operation, so it is outside both OpenAPI contracts and
// registered without a method pattern. Any other shape under /media/ is 404
// instead of reaching the legacy static fallback.
func (s *Server) registerMediaRoutes(mux *http.ServeMux) {
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		s.writeError(w, r, domain.ErrNotFound)
	})
	mux.Handle("/media/", notFound)
	mux.Handle("/media", notFound)
	if s.media != nil {
		mux.HandleFunc("/media/{media}", s.serveMedia)
		mux.HandleFunc("/media/{media}/thumbnail/{sha256}", s.serveMediaThumbnail)
	}
}

// serveMedia delivers one live asset. Its bytes never change under an ID, so
// the content digest is the ETag and caches may keep it for a year; a
// deleted asset is 404 here at once, but copies already cached elsewhere
// live until they expire (ADR 019).
func (s *Server) serveMedia(w http.ResponseWriter, r *http.Request) {
	if !allowMediaRead(w, r) {
		return
	}
	stored, file, err := s.media.Open(r.Context(), r.PathValue("media"))
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		s.writeError(w, r, err)
		return
	}
	defer file.Close()
	writeImmutableMedia(w, r, stored.ContentType, stored.SHA256, file)
}

// serveMediaThumbnail delivers the current thumbnail of a live asset under
// its digest (ADR 022). Any other digest is 404, never a redirect: a URL names
// one set of bytes for good, or nothing.
func (s *Server) serveMediaThumbnail(w http.ResponseWriter, r *http.Request) {
	if !allowMediaRead(w, r) {
		return
	}
	thumbnail, file, err := s.media.OpenThumbnail(r.Context(), r.PathValue("media"), r.PathValue("sha256"))
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		s.writeError(w, r, err)
		return
	}
	defer file.Close()
	writeImmutableMedia(w, r, thumbnail.ContentType, thumbnail.SHA256, file)
}

func allowMediaRead(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func writeImmutableMedia(w http.ResponseWriter, r *http.Request, contentType, digest string, file io.ReadSeeker) {
	header := w.Header()
	header.Set("Content-Type", contentType)
	header.Set("ETag", `"`+digest+`"`)
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// ServeContent answers HEAD, If-None-Match (304) and ranges, and keeps
	// the Content-Type above instead of sniffing.
	http.ServeContent(w, r, "", time.Time{}, file)
}

func (s *Server) registerStaffMediaRoutes(mux *http.ServeMux) {
	mux.Handle("PUT /api/admin/v1/products/{product}/media", s.staffAuth(http.HandlerFunc(s.staffSetProductMedia)))
	mux.Handle("GET /api/admin/v1/media", s.staffAuth(http.HandlerFunc(s.staffListMedia)))
	mux.Handle("POST /api/admin/v1/media", s.staffAuth(http.HandlerFunc(s.staffUploadMedia)))
	mux.Handle("GET /api/admin/v1/media/{media}", s.staffAuth(http.HandlerFunc(s.staffMediaAsset)))
	mux.Handle("DELETE /api/admin/v1/media/{media}", s.staffAuth(http.HandlerFunc(s.staffDeleteMedia)))
}

func (s *Server) staffListMedia(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.MediaRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	query := r.URL.Query()
	page, limit := 0, 0
	fields := []domain.FieldError{}
	for _, param := range []struct {
		name   string
		target *int
	}{{"page", &page}, {"limit", &limit}} {
		if raw := query.Get(param.name); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 {
				fields = append(fields, domain.FieldError{Field: param.name, Message: "Ожидается целое число больше нуля"})
				continue
			}
			*param.target = value
		}
	}
	if len(fields) > 0 {
		s.writeError(w, r, &domain.ValidationError{Fields: fields})
		return
	}
	result, err := s.media.List(r.Context(), page, limit)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) staffMediaAsset(w http.ResponseWriter, r *http.Request) {
	if err := s.authorizeStaffRead(r, application.MediaRead); err != nil {
		s.writeError(w, r, err)
		return
	}
	asset, err := s.media.Get(r.Context(), r.PathValue("media"))
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, asset)
}

func (s *Server) staffUploadMedia(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.MediaUpload)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	data, err := readUploadedFile(w, r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	asset, err := s.media.Upload(r.Context(), mutation, data)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, asset)
}

func (s *Server) staffDeleteMedia(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.MediaDelete)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.media.Delete(r.Context(), mutation, r.PathValue("media")); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func uploadFieldError(message string) error {
	return &domain.ValidationError{Fields: []domain.FieldError{{Field: "file", Message: message}}}
}

// readUploadedFile reads the single "file" part of a bounded multipart body.
// The whole request is capped, and so is the part, so neither a huge body
// nor a huge part is ever buffered.
func readUploadedFile(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	r.Body = http.MaxBytesReader(w, r.Body, media.MaxUploadBytes+multipartOverhead)
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return nil, uploadFieldError("Отправьте изображение как multipart/form-data в поле file")
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	var data []byte
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, uploadReadError(err)
		}
		if part.FormName() != "file" || data != nil {
			return nil, uploadFieldError("Ожидается ровно одно поле file")
		}
		data, err = io.ReadAll(io.LimitReader(part, media.MaxUploadBytes+1))
		if err != nil {
			return nil, uploadReadError(err)
		}
		if len(data) > media.MaxUploadBytes {
			return nil, domain.ErrMediaTooLarge
		}
	}
	if len(data) == 0 {
		return nil, uploadFieldError("Выберите файл изображения")
	}
	return data, nil
}

func uploadReadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return domain.ErrMediaTooLarge
	}
	return uploadFieldError("Не удалось прочитать загрузку")
}

// Return the exact committed revision, never a separate read that could adopt
// another writer's revision while the editor retains its older form fields.
func (s *Server) staffSetProductMedia(w http.ResponseWriter, r *http.Request) {
	mutation, err := s.staffMutation(r, application.ProductMediaUpdate)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body struct {
		ExpectedRevision int64    `json:"expectedRevision"`
		PrimaryID        *string  `json:"primaryId"`
		GalleryIDs       []string `json:"galleryIds"`
	}
	if err := decodeCompleteNullable(w, r, &body, []string{"expectedRevision", "primaryId", "galleryIds"}, []string{"primaryId"}); err != nil {
		s.writeError(w, r, err)
		return
	}
	primary := ""
	if body.PrimaryID != nil {
		primary = *body.PrimaryID
		if primary == "" {
			s.writeError(w, r, &domain.ValidationError{Fields: []domain.FieldError{{Field: "primaryId", Message: "Укажите изображение из медиатеки или null"}}})
			return
		}
	}
	result, err := s.media.SetProductMedia(r.Context(), mutation, body.ExpectedRevision, domain.ProductMedia{
		ProductID: r.PathValue("product"), PrimaryID: primary, GalleryIDs: body.GalleryIDs})
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Revision int64 `json:"revision"`
		domain.ProductMedia
	}{body.ExpectedRevision + 1, result})
}
