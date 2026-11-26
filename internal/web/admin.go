package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/arif-rachim/renoxium/internal/accounts"
	"github.com/arif-rachim/renoxium/internal/appdef"
	"github.com/arif-rachim/renoxium/internal/files"
	"github.com/arif-rachim/renoxium/internal/platform"
)

func (s *Site) registerAdmin() {
	s.mux.HandleFunc("GET /apps/{app}", s.adminApp)
	s.mux.HandleFunc("GET /apps/{app}/cover.jpg", s.appCoverImage)
	s.mux.HandleFunc("GET /apps/{app}/sales", s.adminApp)
	s.mux.HandleFunc("GET /apps/{app}/status", s.adminApp)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}", s.adminList)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}/calendar", s.adminCalendar)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}/new", s.adminForm)
	s.mux.HandleFunc("POST /apps/{app}/t/{table}/new", s.adminSave)
	s.mux.HandleFunc("GET /apps/{app}/t/{table}/{id}", s.adminForm)
	s.mux.HandleFunc("POST /apps/{app}/t/{table}/{id}", s.adminSave)
	s.mux.HandleFunc("POST /apps/{app}/t/{table}/{id}/delete", s.adminDelete)
	s.mux.HandleFunc("GET /apps/{app}/visitors", s.adminVisitors)
	s.mux.HandleFunc("POST /apps/{app}/visitors/{id}/delete", s.adminDeleteVisitor)
	s.mux.HandleFunc("GET /apps/{app}/preview", s.adminPreview)
	s.mux.HandleFunc("GET /apps/{app}/data-stage", s.adminDataStage)
	s.mux.HandleFunc("GET /apps/{app}/preview/{page...}", s.adminPreview)
	s.mux.HandleFunc("GET /apps/{app}/publish", s.publishForm)
	s.mux.HandleFunc("GET /apps/{app}/files/{id}", s.adminPrivateFile)
	s.mux.HandleFunc("POST /apps/{app}/publish", s.publishConfirm)
}

func (s *Site) publishLink(app string, version int) string {
	return fmt.Sprintf("%s/apps/%s/publish?version=%d", s.base, app, version)
}

// publishForm asks the owner to confirm a publish their agent asked for.
func (s *Site) publishForm(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil || version < 1 {
		notFound(w, err)
		return
	}
	if _, _, err := s.platform.Definition(app, version); err != nil {
		notFound(w, err)
		return
	}
	info, err := s.platform.Info(app)
	if err != nil {
		serverError(w, err)
		return
	}
	pl, _ := s.platform.AppPlan(app)
	if pl.Problem == "" {
		pl.Ready = true
	}
	render(w, http.StatusOK, "publish", map[string]any{"Shell": s.shell(r, u, app, "Publish", "", false), "Info": info, "Version": version, "CSRF": s.csrfToken(w, r),
		"Rollback": version < info.LiveVersion, "Plan": pl, "Unsaved": platform.UnsavedSummary(s.platform.UnsavedPreviewChanges(app))})
}

func (s *Site) publishConfirm(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil || version < 1 {
		notFound(w, err)
		return
	}
	res, err := s.platform.Publish(app, version)
	if err != nil {
		render(w, http.StatusConflict, "error", map[string]any{"Signed": true, "Message": "Could not publish: " + err.Error()})
		return
	}
	s.platform.Note(app, fmt.Sprintf("%s published version %d", u.Email, version))
	render(w, http.StatusOK, "notice", map[string]any{"Signed": true, "Title": "Published", "Message": fmt.Sprintf("Version %d is live at %s.", res.LiveVersion, res.LiveURL),
		"Link": res.LiveURL, "LinkText": "Open the live site", "Back": "/apps/" + app})
}

// adminPreview sends the owner to their app's private preview with a
// short-lived link that unlocks it in this browser. /apps/<app>/preview/<page>
// opens that page of the preview, as assistants link to pages they changed.
func (s *Site) adminPreview(w http.ResponseWriter, r *http.Request) {
	if _, app, ok := s.member(w, r); ok {
		next := "/" + r.PathValue("page")
		if r.URL.RawQuery != "" {
			next += "?" + r.URL.RawQuery
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, s.platform.PreviewLinkTo(app, next), http.StatusSeeOther)
	}
}

// previewDataCookie marks, for one app's pages, that the admin panel shows
// and edits the preview's data instead of live data.
const previewDataCookie = "rx_data"

// adminDataStage switches the data pages of an app between live data and
// the preview's test data. Preview data lets the owner add photos and rows
// before the first publish; it is replaced each time the app is changed.
// It only changes what this browser shows, like a filter, so it is a link.
func (s *Site) adminDataStage(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.member(w, r)
	if !ok {
		return
	}
	c := &http.Cookie{Name: previewDataCookie, Value: "preview", Path: "/apps/" + app, HttpOnly: true, Secure: s.secure,
		SameSite: http.SameSiteLaxMode, MaxAge: 12 * 3600}
	if r.URL.Query().Get("stage") != "preview" || s.platform.IsDemo(app) {
		c.Value, c.MaxAge = "", -1
	}
	http.SetCookie(w, c)
	back := r.URL.Query().Get("back")
	if !strings.HasPrefix(back, "/apps/"+app) || strings.HasPrefix(back, "//") {
		back = "/apps/" + app
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// owner returns the signed-in user if they own the app in the path. It has
// already answered the request when ok is false.
func (s *Site) owner(w http.ResponseWriter, r *http.Request) (u accounts.User, app string, ok bool) {
	app = r.PathValue("app")
	u, signedIn := s.currentUser(r)
	if !signedIn {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return u, app, false
	}
	if s.platform.CheckOwner(u.ID, app) != nil {
		render(w, http.StatusNotFound, "error", map[string]any{"Message": "There is no app called " + app + " in your account."})
		return u, app, false
	}
	return u, app, true
}

// member returns the signed-in user if they own the app in the path or are
// on its staff with a confirmed email (staff are matched by email, so an
// unconfirmed address must not get access). It has already answered the
// request when ok is false.
func (s *Site) member(w http.ResponseWriter, r *http.Request) (u accounts.User, app string, ok bool) {
	app = r.PathValue("app")
	u, signedIn := s.currentUser(r)
	if !signedIn {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return u, app, false
	}
	if s.platform.CheckOwner(u.ID, app) == nil || (u.Verified && s.platform.IsStaff(app, u.Email)) {
		return u, app, true
	}
	render(w, http.StatusNotFound, "error", map[string]any{"Message": "There is no app called " + app + " in your account."})
	return u, app, false
}

func (s *Site) isOwnerOf(u accounts.User, app string) bool {
	return s.platform.CheckOwner(u.ID, app) == nil
}

type field struct {
	Name, Label, Help, Type, Input, Value, Error string
	Options                                      []string
	Required, Checked                            bool
	FileName                                     string // file columns: the stored file's name, "" if it is gone
	OwnerOnly                                    bool   // file columns seen by staff: no link, no delete
}

func (s *Site) adminForm(w http.ResponseWriter, r *http.Request) {
	_, app, demo, ok := s.viewer(w, r)
	if !ok {
		return
	}
	table := r.PathValue("table")
	t, err := s.table(r.Context(), app, table)
	if err != nil {
		notFound(w, err)
		return
	}
	var row map[string]any
	var id int64
	if raw := r.PathValue("id"); raw != "" {
		if id, err = strconv.ParseInt(raw, 10, 64); err != nil {
			notFound(w, err)
			return
		}
		if row, err = s.platform.AdminGet(r.Context(), app, table, id); err != nil {
			notFound(w, err)
			return
		}
	}
	if demo && row == nil {
		http.Redirect(w, r, "/apps/"+app+"/t/"+table, http.StatusSeeOther) // no new rows in a read-only demo
		return
	}
	values := map[string]string{}
	for _, c := range t.Columns {
		values[c.Name] = formValue(row[c.Name], c.Type)
	}
	s.renderForm(w, r, http.StatusOK, app, t, id, row, values, nil, r.URL.Query().Get("saved") == "1")
}

func (s *Site) renderForm(w http.ResponseWriter, r *http.Request, status int, app string, t appdef.Table, id int64, row map[string]any,
	values map[string]string, errs platform.FieldErrors, saved bool) {
	u, _ := s.currentUser(r)
	isOwner := s.isOwnerOf(u, app)
	var fields []field
	hasImage := false
	for _, c := range t.Columns {
		f := field{Name: c.Name, Label: columnLabel(c), Help: c.Help, Type: c.Type, Value: values[c.Name], Required: c.Required, Error: errs[c.Name]}
		switch {
		case len(c.Options) > 0:
			f.Input, f.Options = "select", c.Options
			if f.Value != "" && !slices.Contains(c.Options, f.Value) {
				f.Options = append([]string{f.Value}, c.Options...)
			}
		}
		switch c.Type {
		case "boolean":
			f.Input, f.Checked = "checkbox", f.Value == "1"
		case "integer", "real":
			f.Input = "number"
		case "datetime":
			f.Input = "datetime-local"
		case "json":
			f.Input = "textarea"
		case "image":
			f.Input = "image"
			hasImage = true
		case "file":
			f.Input = "file"
			f.OwnerOnly = !isOwner
			if pf, err := s.platform.PrivateFileInfo(app, s.platform.AdminStageFor(r.Context(), app), f.Value); err == nil {
				f.FileName = pf.Name
				if !isOwner {
					f.FileName = "Document"
				}
			}
		default:
			if f.Input == "select" {
				break
			}
			f.Input = "text"
			if len(f.Value) > 80 || strings.ContainsAny(f.Value, "\n") || longText(c.Name) {
				f.Input = "textarea"
			}
		}
		fields = append(fields, f)
	}
	var other string
	for k, v := range errs {
		if !hasColumn(t, k) {
			other = label(k) + ": " + v
		}
	}
	liveURL := ""
	if info, err := s.platform.Info(app); err == nil {
		liveURL = info.LiveURL
	}
	created := ""
	if row != nil {
		created = display(row["created_at"], "datetime")
	}
	render(w, status, "admin_form", map[string]any{
		"App": app, "Table": t.Name, "Label": tableLabel(t), "ID": id, "Fields": fields, "Created": created,
		"CSRF": s.csrfToken(w, r), "Saved": saved, "Error": other, "HasErrors": len(errs) > 0,
		"HasImage": hasImage, "LiveURL": liveURL, "Back": calendarBack(r, app, t.Name), "Notice": s.hookNotice(r, app),
		"Demo": s.platform.IsDemo(app), "Shell": s.shell(r, u, app, tableLabel(t), "/apps/"+app+"/t/"+t.Name, s.platform.IsDemo(app)),
	})
}

// hookNotice is the notice about a failed on_admin_change run after this
// person's last change, shown once on the next admin page.
func (s *Site) hookNotice(r *http.Request, app string) string {
	u, ok := s.currentUser(r)
	if !ok {
		return ""
	}
	return s.platform.AdminNotice(app, u.Email)
}

func (s *Site) adminSave(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.member(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, files.MaxUpload+1<<20) // one camera photo, shrunk on the way in
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				render(w, http.StatusRequestEntityTooLarge, "error", map[string]any{"Message": files.ErrPhotoTooLarge.Error() + ". Go back and choose a smaller image, or save the photos one at a time."})
				return
			}
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		defer r.MultipartForm.RemoveAll()
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	table := r.PathValue("table")
	t, err := s.table(r.Context(), app, table)
	if err != nil {
		notFound(w, err)
		return
	}
	var id int64
	if raw := r.PathValue("id"); raw != "" {
		if id, err = strconv.ParseInt(raw, 10, 64); err != nil {
			notFound(w, err)
			return
		}
	}
	values := map[string]string{}
	uploadErrs := platform.FieldErrors{}
	var dropFiles []string // visitor files the owner chose to delete, once the row is saved
	isOwner := s.isOwnerOf(u, app)
	var stored map[string]any // the row as saved, for file columns staff may not change
	if !isOwner && id != 0 {
		stored, _ = s.platform.AdminGet(r.Context(), app, table, id)
	}
	for _, c := range t.Columns {
		values[c.Name] = r.PostFormValue(c.Name)
		if c.Type == "file" && !isOwner {
			// Staff can neither remove a visitor's document nor point the
			// row at another one: the stored value stays.
			values[c.Name] = formValue(stored[c.Name], c.Type)
			continue
		}
		if c.Type == "file" && r.PostFormValue(c.Name+"__delete") == "1" {
			dropFiles = append(dropFiles, values[c.Name])
			values[c.Name] = ""
		}
		if c.Type != "image" {
			continue
		}
		if r.PostFormValue(c.Name+"__remove") == "1" {
			values[c.Name] = ""
		}
		if file, _, err := r.FormFile(c.Name + "__upload"); err == nil {
			info, err := s.platform.AddFile(app, file)
			file.Close()
			if err != nil {
				uploadErrs[c.Name] = err.Error()
				continue
			}
			values[c.Name] = info.URL
		}
	}
	var newID int64
	if len(uploadErrs) > 0 {
		err = uploadErrs
	} else {
		newID, err = s.platform.AdminSave(r.Context(), app, table, id, values, u.Email)
	}
	var ferr platform.FieldErrors
	switch {
	case errors.As(err, &ferr):
		var row map[string]any
		if id != 0 {
			row, _ = s.platform.AdminGet(r.Context(), app, table, id)
		}
		s.renderForm(w, r, http.StatusUnprocessableEntity, app, t, id, row, values, ferr, false)
	case errors.Is(err, platform.ErrNotFound):
		notFound(w, err)
	case err != nil:
		serverError(w, err)
	default:
		for _, id := range dropFiles {
			if s.platform.DeletePrivateFile(app, s.platform.AdminStageFor(r.Context(), app), id) == nil {
				s.platform.Note(app, fmt.Sprintf("%s deleted uploaded file %s", u.Email, id))
			}
		}
		next := fmt.Sprintf("/apps/%s/t/%s/%d?saved=1", app, table, newID)
		if back := calendarBack(r, app, table); back != "" {
			next += "&back=" + url.QueryEscape(back)
		}
		http.Redirect(w, r, next, http.StatusSeeOther)
	}
}

// adminPrivateFile streams a visitor's private upload of the live app to
// its owner or staff, signed in to the Renoxium site; the app host only
// ever shows a file to the visitor who uploaded it.
//
// Owner only: these are visitors' personal documents (passports, licences),
// and staff are matched by an email address the owner typed. Staff get 404,
// as if the file did not exist.
func (s *Site) adminPrivateFile(w http.ResponseWriter, r *http.Request) {
	_, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	f, fh, err := s.platform.OpenPrivateFile(app, s.platform.AdminStageFor(r.Context(), app), r.PathValue("id"))
	if err != nil {
		notFound(w, err)
		return
	}
	defer fh.Close()
	platform.ServePrivateFile(w, r, f, fh)
}

// privateFileName is what the table list shows for a file column.
func (s *Site) privateFileName(ctx context.Context, app string, v any) string {
	id, _ := v.(string)
	if id == "" {
		return ""
	}
	if f, err := s.platform.PrivateFileInfo(app, s.platform.AdminStageFor(ctx, app), id); err == nil {
		return f.Name
	}
	return "(file deleted)"
}

func (s *Site) adminDelete(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.member(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	table := r.PathValue("table")
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		notFound(w, err)
		return
	}
	if err := s.platform.AdminDelete(r.Context(), app, table, id, u.Email); err != nil {
		if errors.Is(err, platform.ErrNotFound) {
			notFound(w, err)
			return
		}
		render(w, http.StatusConflict, "error", map[string]any{"Message": "Could not delete: " + err.Error()})
		return
	}
	next := "/apps/" + app + "/t/" + table
	if back := calendarBack(r, app, table); back != "" {
		next = back
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Site) adminDeleteVisitor(w http.ResponseWriter, r *http.Request) {
	u, app, ok := s.owner(w, r)
	if !ok {
		return
	}
	if !s.checkCSRF(r) {
		http.Error(w, "the form expired; go back and try again", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		notFound(w, err)
		return
	}
	if err := s.platform.DeleteVisitor(r.Context(), app, id, u.Email); err != nil {
		notFound(w, err)
		return
	}
	http.Redirect(w, r, "/apps/"+app+"/visitors", http.StatusSeeOther)
}

func (s *Site) table(ctx context.Context, app, name string) (appdef.Table, error) {
	tables, err := s.platform.TablesFor(ctx, app)
	if err != nil {
		return appdef.Table{}, err
	}
	for _, t := range tables {
		if t.Name == name {
			return t, nil
		}
	}
	return appdef.Table{}, platform.ErrNotFound
}

func hasColumn(t appdef.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func notFound(w http.ResponseWriter, err error) {
	render(w, http.StatusNotFound, "error", map[string]any{"Message": "Not found."})
}

func serverError(w http.ResponseWriter, err error) {
	render(w, http.StatusInternalServerError, "error", map[string]any{"Message": "Something went wrong. Try again in a moment."})
}

// label turns price_aed into "Price aed".
func tableLabel(t appdef.Table) string {
	if t.Label != "" {
		return t.Label
	}
	return label(t.Name)
}

func columnLabel(c appdef.Column) string {
	if c.Label != "" {
		return c.Label
	}
	return label(c.Name)
}

func label(name string) string {
	s := strings.ReplaceAll(name, "_", " ")
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func longText(name string) bool {
	for _, w := range []string{"description", "notes", "note", "address", "body", "content", "message", "details"} {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// display formats a stored value for reading.
func display(v any, typ string) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		if typ == "image" && x != "" {
			return "Image"
		}
		return x
	case int64:
		if typ == "boolean" {
			if x != 0 {
				return "Yes"
			}
			return "No"
		}
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// formValue formats a stored value for an input field.
func formValue(v any, typ string) string {
	switch typ {
	case "image", "file":
		s, _ := v.(string)
		return s
	case "boolean":
		if x, ok := v.(int64); ok && x != 0 {
			return "1"
		}
		return ""
	case "datetime":
		s, _ := v.(string)
		if len(s) >= 16 {
			return strings.Replace(s[:16], " ", "T", 1)
		}
		return s
	}
	return display(v, typ)
}

func short(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}
